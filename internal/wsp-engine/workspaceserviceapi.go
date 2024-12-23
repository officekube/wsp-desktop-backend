package workspaceEngine

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Interface
type IWorkspaceService interface {
	Init()	(error *error)
	GetStartupWorkflows(workspaceId string, accessToken string) ([]AWorkflow, *error)
	GetUserToken(workspaceId string, wspEngineToken string) (string, *error)
	GetWspEngineSettings(accessToken string, wspId string) ([]byte, error)
	GetUpdateCheck(workspaceId string, updateCheckRequest *UpdateCheckRequest) (*UpdateCheckResponse, error)
}

type BaseWorkspaceService struct {
	IWorkspaceService
	UserAccessToken *string
}

var WorkspaceService IWorkspaceService

func InitBaseWorkspaceService() *error {
	WorkspaceService = &BaseWorkspaceService{}
	return WorkspaceService.Init()
}

func (ws *BaseWorkspaceService) Init() (err *error) {
	return nil
}

func (ws *BaseWorkspaceService) GetStartupWorkflows(workspaceId string, accessToken string) ([]AWorkflow, *error) {
	var result []AWorkflow
	client := http.Client{}
	req, err := http.NewRequest("GET", Configuration.WorkspaceService.Endpoint + "/workspaces/" + workspaceId, nil)
	if err != nil {
		log.Println("Failed to prep a call to the workspace service /workspaces/{workspaceId} API.")
		return result, &err
	}

	req.Header.Add("Authorization", "Bearer " + accessToken)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)

	if err != nil {
		log.Println("Failed to call to the workspace service /workspaces/{workspaceId} API.")
		return result, &err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		err := errors.New("The workspace service /workspaces/{workspaceId} rejected with the message: " + resp.Status)
		return result, &err
	} else {
		bodyBytes, _ := io.ReadAll(resp.Body)
		var wsp *AWorkspace
		json.Unmarshal(bodyBytes, &wsp)
		result = wsp.Workflows
		return result, nil
	}

}

func (ws *BaseWorkspaceService) GetUserToken(workspaceId string, wspEngineToken string) (string, *error) {
	var result string

	//log.Println("Received request for the user token")
	if ws.UserAccessToken != nil {
		// Check the expiration time and if it is not in the past return the existing token
		token, _, err := new(jwt.Parser).ParseUnverified(*ws.UserAccessToken, jwt.MapClaims{})
		if err == nil {
			exp, ok := token.Claims.(jwt.MapClaims)["exp"].(float64)
			if ok {
				expTime := time.Unix(int64(exp), 0)
				if time.Now().Before(expTime) {
					//log.Println("Returning the existing user token")
					return *ws.UserAccessToken, nil
				}
			}
		}
	}

	client := http.Client{}
	req, err := http.NewRequest("GET", Configuration.WorkspaceService.Endpoint + "/workspaces/" + workspaceId + "/usertoken", nil)
	if err != nil {
		log.Println("Failed to prep a call to the workspace service /workflows/{workspaceId} API.")
		return result, &err
	}

	req.Header.Add("Authorization", "Bearer " + wspEngineToken)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)

	if err != nil {
		log.Println("Failed to call to the workspace service /workflows/{workspaceId}/usertoken API.")
		return result, &err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Println("The workspace service /workflows/{workspaceId}/usertoken rejected with the message: " + resp.Status)
		err := errors.New("The workspace service /workflows/{workspaceId}/usertoken rejected with the message: " + resp.Status)
		return result, &err
	} else {
		bodyBytes, _ := io.ReadAll(resp.Body)
		var pl UserTokenPayload
		json.Unmarshal(bodyBytes, &pl)		
		result = pl.Token
		ws.UserAccessToken = &result
		return result, nil
	}
}

type UserTokenPayload struct {
	Token	string `json: token`
}

func (ws *BaseWorkspaceService) GetWspEngineSettings(accessToken string, wspId string) ([]byte, error) {
	var result []byte
	client := http.Client{}
	//FIXME: change the wsp service endpoint to accept parameters of version and OS so that the service returns configuration that is version and OS specific.
	req, err := http.NewRequest("GET", Configuration.WorkspaceService.Endpoint + "/workspaces/" + wspId + "/engine/settings", nil)
	if err != nil {
		log.Println("Failed to prep a call to the workspace service /workspaces/engine/settings API.")
		return result, err
	}

	req.Header.Add("Authorization", "Bearer " + accessToken)

	resp, err := client.Do(req)

	if err != nil {
		log.Println("Failed to call to the workspace service /workspaces/engine/settings API.")
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		err := errors.New("The workspace service /workspaces/engine/settings rejected with the message: " + resp.Status)
		return result, err
	} else {
		result, _ := io.ReadAll(resp.Body)
		return result, nil
	}

}

func (ws *BaseWorkspaceService) GetUpdateCheck(workspaceId string, updateCheckRequest *UpdateCheckRequest) (*UpdateCheckResponse, error) {

	jsonPayload, err := json.Marshal(updateCheckRequest)
	if err != nil {
		log.Println("Failed to marshal update check payload:", err)
		return nil, err
	}

	client := http.Client{}
	endpoint := Configuration.WorkspaceService.Endpoint;
	req, err := http.NewRequest("POST", endpoint+"/workspaces/"+workspaceId+"/engine/checkupdate", bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Println("Failed to prep a call to the workspace service /workspaces/"+workspaceId+"/engine/checkupdate API:", err)
		return nil, err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Failed to call the workspace service /workspaces/"+workspaceId+"/engine/checkupdate API:", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Println("The workspace service /workspaces/"+workspaceId+"/engine/checkupdate API rejected with the message:", resp.Status)
		return nil, errors.New("The workspace service /workspaces/"+workspaceId+"/engine/checkupdate API rejected with the message: "+resp.Status)
	}

	var updateResponse UpdateCheckResponse
	bodyBytes, _ := io.ReadAll(resp.Body)
	err = json.Unmarshal(bodyBytes, &updateResponse)
	if err != nil {
		log.Println("Failed to unmarshal update check response:", err)
		return nil, err
	}

	return &updateResponse, nil
}