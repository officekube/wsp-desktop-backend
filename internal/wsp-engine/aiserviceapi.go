package workspaceEngine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
)

const promptEstimatePath = "/prompts/estimate"

type AiService struct {
}

var AiServiceApi = &AiService{}

// Estimate makes a call to AI service to get the average cost of the prompt to be executed by the AI service
func (as *AiService) GetEstimate(w AWorkflow, bearerToken string) (*WorkflowEstimate, error) {
	if len(bearerToken) == 0 {
		err := errors.New("Access token must be provided")
		return nil, err
	}

	prompt := ""

	for _, p := range w.Parameters {
		if p.Name == "ok_prompt" {
			prompt = p.ActualValues[0]
			break
		}
	}

	if len(prompt) == 0 {
		err := errors.New("Paramater ok_prompt can NOT be empty.")
		return nil, err
	}

	var pr PromptEstimateRequest
	pr.Prompt = prompt
	pr.WorkflowId = strconv.Itoa(int(w.Id))

	promptRequest, _ := json.Marshal(pr)
	client := http.Client{}
	req, err := http.NewRequest("POST", Configuration.AiService.Endpoint+promptEstimatePath, bytes.NewBuffer(promptRequest))
	if err != nil {
		log.Println("Failed to create a request for the prompt estimate request")
		return nil, err
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", bearerToken)

	resp, err := client.Do(req)
	if err != nil {
		log.Println("Failed to get the estimate for the prompt with workflowId: " + strconv.Itoa(int(w.Id)))
		return nil, err
	}

	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		log.Println(fmt.Sprintf("The request to get an estimate for workflowId: %s failed with status code: %d", w.Id, resp.StatusCode))
		return nil, err
	}

	var promptEstRes PromptEstimateResponse
	body, _ := io.ReadAll(resp.Body)
	err = json.Unmarshal(body, &promptEstRes)
	if err != nil {
		log.Println("Failed to parse the response from prompt estimate: " + err.Error())
		return nil, err
	}
	var we WorkflowEstimate
	we.Amount = promptEstRes.Cost
	we.Units = fmt.Sprintf("%.2f", promptEstRes.ConsumedTokens)
	we.UoM = "tokens"
	we.Status = promptEstRes.Status
	switch promptEstRes.Status {
		case "high":
			we.Message = "The prompt will cost around"
		case "med":
			we.Message = "The prompt will roughly cost around"
		case "none":
			we.Message = "The prompt could not be estimated."
	}
	return &we, nil
}
