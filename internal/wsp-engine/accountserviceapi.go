package workspaceEngine

import (
	"encoding/json"
	"errors"
	"io"

	"log"
	"net/http"
)

type AccountServiceObject struct {
}

var AccountService = &AccountServiceObject{}

/**
 * The function will retrieve a user's personal platform token. To ensure secure retrieval it will be pulled from the account service,
 * and not from the DB.
 */
 func (as *AccountServiceObject) GetUserPlatformPersonalToken(bearer_token string) (string, *error) {
	var result string

	client := http.Client{}
	req, err := http.NewRequest("GET", Configuration.AccountService.Endpoint + "/users/current/personaltoken", nil)
	if err != nil {
		log.Println("Failed to prep a call to the account service endpoint.")
		return result, &err
	}

	req.Header.Add("Authorization", bearer_token)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)

	if err != nil {
		log.Println("Failed to call to the account service endpoint.")
		return result, &err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Println("The account service rejected the request with the status code: " + resp.Status)
		err = errors.New("The account service rejected the request with the status code: " + resp.Status)
		return result, &err
	} else {
		// Read the response
		bodyBytes, _ := io.ReadAll(resp.Body)

		var returnResult *AResult
		json.Unmarshal(bodyBytes, &returnResult)
		if returnResult.Code != 0 {
			err = errors.New(returnResult.Message)
			return result, &err
		} else {
			result = returnResult.Message
			return result, nil
		}

	}
}

