package workspaceEngine

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/jinzhu/copier"
	"gorm.io/gorm/clause"
)

type PlatformRepoAPI struct {
}

var PlatformRepo = &PlatformRepoAPI{}

func (plr *PlatformRepoAPI) GetRepoFileContent(token string, projectId int, fileName string) (string, int) {
	var returnErr int
	var content string

	client := http.Client{}
	req, err := http.NewRequest("GET", Configuration.Gitlab.BaseUrl+"/projects/"+strconv.Itoa(projectId)+"/repository/files/"+fileName+"/raw?ref="+Configuration.Gitlab.WorkflowRepoBranch, nil)
	if err != nil {
		log.Println("Failed to prep a call to the OK Platform Repo API.")
		returnErr = 1
		return content, returnErr
	}

	req.Header.Add("Authorization", "Bearer "+token)

	resp, err := client.Do(req)

	if err != nil {
		log.Println("Failed to call the OK Platform Repo API.")
		log.Println(err)
		returnErr = 2
		return content, returnErr
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Println("Unsuccessful call the OK Platform Repo API.")
		returnErr = 3
		return content, returnErr
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	content = string(bodyBytes)

	return content, returnErr
}

func (plr *PlatformRepoAPI) GetPlatformRepo(workflow *Workflow, token string) *AWorkflow {
	var project *AWorkflow
	// Check in the platform-workflows group first
	project = plr.searchPlatformRepoByGroup(token, workflow.ExternalId)
	if project == nil {
		project = plr.searchPlatformRepoByGroup(token, workflow.ExternalId)
	}
	if project != nil {
		// Pull parameters
		db, err := GetDBConnection()
		if err != nil {
			return project
		}
		var existingParams []WorkflowParameter
		db.Table("workflow_parameters").Where(clause.Eq{Column: "workflow_id", Value: workflow.Id}).Find(&existingParams)
		if len(existingParams) > 0 {
			project.Parameters = make([]AWorkflowParameter, len(existingParams))
		}
		for i, param := range existingParams {
			var p AWorkflowParameter
			copier.Copy(&p, &param)
			project.Parameters[i] = p
		}
		// Pull Schedule
		var schedule WorkflowSchedule
		result := db.Table("workflow_schedules").Where(clause.Eq{Column: "workflow_id", Value: workflow.Id}).
			Where(clause.Eq{Column: "name", Value: workflow.Name}).First(&schedule)
		if result.RowsAffected == 1 {
			copier.Copy(&project.Schedule, schedule)
		}
	}
	return project
}

func (okplr *PlatformRepoAPI) searchPlatformRepoByGroup(token string, projectId int64) *AWorkflow {
	var project *AWorkflow

	client := http.Client{}
	req, err := http.NewRequest("GET", Configuration.Workflow.GitBaseUrl+"api/v4/projects/"+strconv.Itoa((int)(projectId)), nil)
	if err != nil {
		log.Println("Failed to prep a call to the Platform Repo API.")
		return project
	}

	req.Header.Add("Authorization", "Bearer "+token)
	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)

	if err != nil {
		log.Println("Failed to call the Platform Repo API.")
		return project
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Println("Unsuccessful call the Platform Repo API.")
		return project
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	json.Unmarshal(bodyBytes, &project)

	return project
}
