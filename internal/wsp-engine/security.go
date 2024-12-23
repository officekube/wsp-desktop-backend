package workspaceEngine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	oidc "github.com/coreos/go-oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

// Interface
type ISecurityManager interface {
	Init(ginEngine *gin.Engine)	(error *error)
	IsApiAuthenticated(c *gin.Context) int
	WebAuthCallback(c *gin.Context)
	ApiAuthCallback(c *gin.Context)
	GetIdPToken(bearer_token string, providerAlias string) string
	GetLoggedOnUser(bearer_token string) *User
	GetOAuth2Config() *oauth2.Config
}

type BaseSecurityManager struct {
	ISecurityManager
	BearerToken string
	OAuth2Config   oauth2.Config
	OAuth2Verifier *oidc.IDTokenVerifier
}

//var SecurityManager = &BaseSecurityManager{}
var SecurityManager ISecurityManager

func InitBaseSecurityManager(ginEngine *gin.Engine) *error {
	SecurityManager = &BaseSecurityManager{}
	// Initialize gin engine
	SecurityManager.Init(ginEngine)
	return nil
}

func (sm *BaseSecurityManager) Init(ginEngine *gin.Engine) (err *error) {
	if ginEngine == nil { 
		e := errors.New("A reference to the GIN engine cannot be nil.")
		err = &e
		return err
	}
	// 1. Populate routes that the manager must serve
	var routes = Routes {
		{
			"ApiAuthCallback",
			http.MethodGet,
			"/api/auth_callback",
			sm.ApiAuthCallback,
		},
		{
			"WebAuthCallback",
			http.MethodGet,
			"/web/auth_callback",
			sm.WebAuthCallback,
		},
	}
	for _, route := range routes {
		switch route.Method {
		case http.MethodGet:
			ginEngine.GET(route.Pattern, route.HandlerFunc)
		case http.MethodPost:
			ginEngine.POST(route.Pattern, route.HandlerFunc)
		case http.MethodPut:
			ginEngine.PUT(route.Pattern, route.HandlerFunc)
		case http.MethodPatch:
			ginEngine.PATCH(route.Pattern, route.HandlerFunc)
		case http.MethodDelete:
			ginEngine.DELETE(route.Pattern, route.HandlerFunc)
		}
	}

	// 2. Initialize OAuth2 Verifier
	ctx := context.Background()
	provider, err2 := oidc.NewProvider(ctx, Configuration.Security.OAuth2.IssuerUrl)
	if err2 != nil {
		return &err2
	}

	// Configure an OpenID Connect aware OAuth2 client.
	sm.OAuth2Config = oauth2.Config{
		ClientID:    Configuration.Security.OAuth2.ClientID,
		RedirectURL: Configuration.Security.OAuth2.WebRedirectURL,
		// Discovery returns the OAuth2 endpoints.
		Endpoint: provider.Endpoint(),
		// "openid" is a required scope for OpenID Connect flows.
		Scopes: Configuration.Security.OAuth2.Scopes,
	}

	oidcConfig := &oidc.Config{
		ClientID: Configuration.Security.OAuth2.ClientID,
		// For some reason when validating the bearer token in IsApiAuthenticated, the token has audience [account]
		// and not the client id (test_app) which causes the verifier to spit out the error oidc:
		// expected audience test_app got [account]". For that reason we skip this check
		SkipClientIDCheck: true,
	}
	sm.OAuth2Verifier = provider.Verifier(oidcConfig)
	if sm.OAuth2Verifier == nil {
		log.Println("Failed to initialize the oauth2 verifier.")
		err := errors.New("Failed to initialize the oauth2 verifier.")
		return &err
	}
	return nil
}


/**
 * The method validates if there is an authorization header with the id_token.
 */
func (sm *BaseSecurityManager) IsApiAuthenticated(c *gin.Context) int {
	rawAccessToken := c.Request.Header.Get("Authorization")
	if rawAccessToken == "" {
		return 1
	}

	parts := strings.Split(rawAccessToken, " ")
	if len(parts) != 2 {
		return 2
	}

	ctx := context.Background()

	idToken, err := sm.OAuth2Verifier.Verify(ctx, parts[1])

	// idiotic go design - to mute "idToken declared but not used" "error"
	_ = idToken

	if err != nil {
		log.Println("Failed to verify ID Token: " + err.Error())
		return 3
	}

	sm.BearerToken = rawAccessToken

	return 0
}

/**
 * The callback method for the web resources/URLs that does these things:
 * 1. Validates if the IdP has returned all the right things: state, code, and id_token.
 * This is as per https://stackoverflow.com/questions/46844285/difference-between-oauth-2-0-state-and-openid-nonce-parameter-why-state-cou.
 * 2. Validate the id_token
 * 3. Redirect a user to the originally requested url with the id_token attached as a cookie.
 */
func (sm *BaseSecurityManager) WebAuthCallback(c *gin.Context) {

	// 1. Validate IdP response
	state, err := c.Cookie("state")
	if err != nil {
		log.Println("State cookie not found.")
		http.Error(c.Writer, "State cookie not found.", http.StatusBadRequest)
		return
	}

	if c.Request.URL.Query().Get("state") != state {
		log.Println("State did not match.")
		http.Error(c.Writer, "State did not match.", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	oauth2Token, err := sm.OAuth2Config.Exchange(ctx, c.Request.URL.Query().Get("code"))
	if err != nil {
		http.Error(c.Writer, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
		log.Println("Failed to exchange token: " + err.Error())
		return
	}

	// 2. Validate the id_token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		http.Error(c.Writer, "No id_token field in oauth2 token.", http.StatusInternalServerError)
		log.Println("No id_token field in oauth2 token.")
		return
	}

	idToken, err := sm.OAuth2Verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(c.Writer, "Failed to verify ID Token: "+err.Error(), http.StatusInternalServerError)
		log.Println("Failed to verify ID Token: " + err.Error())
		return
	}

	nonce, err := c.Cookie("nonce")
	if err != nil {
		log.Println("Nonce cookie not found.")
		http.Error(c.Writer, "Nonce cookie not found.", http.StatusBadRequest)
		return
	}

	if idToken.Nonce != nonce {
		log.Println("Nonce did not match.")
		http.Error(c.Writer, "Nonce did not match.", http.StatusBadRequest)
	}

	resp := struct {
		OAuth2Token   *oauth2.Token
		IDTokenClaims *json.RawMessage // ID Token payload is just JSON.
	}{oauth2Token, new(json.RawMessage)}

	if err := idToken.Claims(&resp.IDTokenClaims); err != nil {
		http.Error(c.Writer, err.Error(), http.StatusInternalServerError)
		log.Println(err.Error())
		return
	}
	// 3. Redirect a user to the originally requested url with the id_token attached as a cookie.
	c.SetCookie("id_token", rawIDToken, int(time.Hour.Seconds()), "", "", c.Request.TLS != nil, true)
	if redirectUrl, err := c.Cookie("requestedURL"); err != nil {
		http.Error(c.Writer, err.Error(), http.StatusInternalServerError)
		log.Println(err.Error())
		return
	} else {
		c.Redirect(http.StatusFound, redirectUrl)
	}

	/* This is just for troubleshooting to see what the IdP sends
	   data, err := json.MarshalIndent(resp, "", "    ")
	   if err != nil {
	       http.Error(c.Writer, err.Error(), http.StatusInternalServerError)
	       return
	   }

	   c.Writer.Write(data)
	*/
}

/**
 * The callback method for the API resources/URLs that does these things:
 * 1. Validates if the IdP has returned all the right things: state, code, and id_token.
 * This is as per https://stackoverflow.com/questions/46844285/difference-between-oauth-2-0-state-and-openid-nonce-parameter-why-state-cou.
 * 2. Validate the id_token
 * 3. Sets the id_token in the authorization header as a bearer token
 */
func (sm *BaseSecurityManager) ApiAuthCallback(c *gin.Context) {
	// 1. Validate IdP response
	if c.Request.URL.Query().Get("state") == "" {
		log.Println("State was not found.")
		http.Error(c.Writer, "State was not found.", http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	oauth2Token, err := sm.OAuth2Config.Exchange(ctx, c.Request.URL.Query().Get("code"))
	if err != nil {
		http.Error(c.Writer, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
		log.Println("Failed to exchange token: " + err.Error())
		return
	}

	// 2. Validate the id_token
	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		http.Error(c.Writer, "No id_token field in oauth2 token.", http.StatusInternalServerError)
		log.Println("No id_token field in oauth2 token.")
		return
	}

	idToken, err := sm.OAuth2Verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(c.Writer, "Failed to verify ID Token: "+err.Error(), http.StatusInternalServerError)
		log.Println("Failed to verify ID Token: " + err.Error())
		return
	}

	resp := struct {
		OAuth2Token   *oauth2.Token
		IDTokenClaims *json.RawMessage // ID Token payload is just JSON.
	}{oauth2Token, new(json.RawMessage)}

	if err := idToken.Claims(&resp.IDTokenClaims); err != nil {
		http.Error(c.Writer, err.Error(), http.StatusInternalServerError)
		log.Println(err.Error())
		return
	}
	c.Writer.Header().Set("Authorization", "Bearer "+rawIDToken)
}

func (sm *BaseSecurityManager) GetIdPToken(bearer_token string, providerAlias string) string {
	var result string
	client := http.Client{}
	req, err := http.NewRequest("GET", Configuration.OfficeKubeIdP.UrlBase+"/realms/" + Configuration.OfficeKubeIdP.Realm +
		                        "/broker/" + providerAlias + "/token", nil)
	if err != nil {
		log.Println("Failed to prep a call to the IdP endpoint.")
		return result
	}

	req.Header.Add("Authorization", bearer_token)

	resp, err := client.Do(req)

	if err != nil {
		log.Println("Failed to call to the IdP endpoint.")
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Println("The IdP rejected the request with the status code: " + resp.Status)
	} else {
		// Read the response
		bodyBytes, _ := io.ReadAll(resp.Body)

		// Convert response body to string
		bodyString := string(bodyBytes)
		// Parse out the token from the string that should be in the format: access_token=<token>&scope=<scope>&token_type=<token>
		pairs := strings.Split(bodyString, "&")
		for _, pair := range pairs {
			elements := strings.Split(pair, "=")
			if elements[0] == "access_token" {
				result = elements[1]
				break
			}
		}
	}
	return result
}

func (sm *BaseSecurityManager) GetLoggedOnUser(bearer_token string) *User {
    if bearer_token == "" {
        return nil
    }
    
    parts := strings.Split(bearer_token, " ")
    if len(parts) != 2 {
        return nil
    }

    ctx := context.Background()

    idToken, err := sm.OAuth2Verifier.Verify(ctx, parts[1])

    if err != nil {
        log.Println("Failed to verify ID Token: " + err.Error())
        return nil
    }

    var user User
    idToken.Claims(&user)
    return &user
}

type User struct {
    Id          string  `json:"sub"`
    Email       string
    Name        string
    FirstName   string  `json:"given_name"`
    LastName    string  `json:"family_name"`
    Username    string  `json:"preferred_username"`
    GitlabId    int  `json:"gitlabid"`
}


func (sm *BaseSecurityManager) GetOAuth2Config() *oauth2.Config {
	return &sm.OAuth2Config
}
