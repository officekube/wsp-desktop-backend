package workspaceEngine

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const STATUS_INVALID_AWORKFLOW = "INVALID_AWORKFLOW"
const STATUS_INVALID_AWORKFLOW_SCHEDULE = "INVALID_AWORKFLOW_SCHEDULE"
const STATUS_FAILED_TO_LOAD_CONFIG = "FAILED_TO_LOAD_CONFIG"
const STATUS_FAILED_TO_INSTALL_WORKFLOW = "FAILED_TO_INSTALL_WORKFLOW"
const STATUS_FAILED_TO_PARSE_WORKSPACE_ID = "FAILED_TO_PARSE_WORKSPACE_ID"
const STATUS_WORKFLOW_TYPE_IS_MISSING = "WORKFLOW_TYPE_IS_MISSING"
const STATUS_WORKFLOW_TARGET_IS_MISSING = "WORKFLOW_TARGET_IS_MISSING"
const STATUS_WORKFLOW_TYPE_IS_NOT_SUPPORTED = "WORKFLOW_TYPE_IS_NOT_SUPPORTED"
const STATUS_WORKFLOW_TARGET_IS_NOT_SUPPORTED = "WORKFLOW_TARGET_IS_NOT_SUPPORTED"

const STATUS_FAILED_TO_EXECUTE_TASK = "FAILED_TO_EXECUTE_TASK"
const STATUS_FAILED_TO_SAVE = "FAILED_TO_SAVE"
const STATUS_FAILED_TO_CONNECT_TO_DB = "FAILED_TO_CONNECT_TO_DB"

const STATUS_OK = "STATUS_OK"

const STATUS_FAILED_TO_RETRIEVE_PERSONAL_TOKEN = "STATUS_FAILED_TO_RETRIEVE_PERSONAL_TOKEN"

const STATUS_FAILED_TO_SCHEDULE = "STATUS_FAILED_TO_SCHEDULE_TASK"

const STATUS_INVALID_APP = "INVALID_APP"

const STATUS_INVALID_PROMPT = "INVALID_PROMPT"
const STATUS_FAILED_EXECUTE_ESTIMATE = "FAILED_PROMPT_ESTIMATE"

const STATUS_FAILED_TO_INSTALL_APP = "Failed to install"
const STATUS_FAILED_TO_EXECUTE_APP = "Failed to execute"
const STATUS_FAILED_TO_START_APP = "Failed to start"
const STATUS_FAILED_TO_STOP_APP = "Failed to stop"
const STATUS_APP_STARTED = "started"
const STATUS_APP_STOPPED = "stopped"
const STATUS_APP_EXECUTED = "executed"
const STATUS_APP_INSTALLED = "installed"
const STATUS_APP_UNINSTALLED = "uninstalled"

const STATUS_TO_BE_EXECUTED = "WORKFLOW_TO_BE_EXECUTED"
const STATUS_TO_BE_SCHEDULED = "WORKFLOW_TO_BE_SCHEDULED"
const STATUS_INSTALLED = "WORKFLOW_INSTALLED"
const STATUS_EXECUTED = "WORKFLOW_EXECUTED"

const STATUS_APP_TO_BE_INSTALLED = "to be installed"
const STATUS_APP_TO_BE_EXECUTED = "to be executed"
const STATUS_APP_TO_BE_SCHEDULED = "to be scheduled"

const STATUS_FAILED_TO_CHECK_A_DEPENDENCY_PACKAGE = "Failed to check a dependency package"

const STATUS_INVALID_USED_WORKFLOW = "INVALID_USED_WORKFLOW"
const STATUS_FAILED_TO_RENDER_PROMPT_WORKFLOW = "FAILED_TO_RENDER_PROMPT_WORKFLOW"
const STATUS_FAILED_TO_ESTIMATE_WORKFLOW = "FAILED_TO_ESTIMATE_WORKFLOW"

// Interface
type IErrorHandler interface {
	Init()	(error *error)
	ReportError(errorCode string, msg string, updateStatus bool, workflow *Workflow, aglp *AWorkflow, report bool, bearer_token *string, alert bool) *gin.H
	ReportAppError(errorCode string, msg string, uApp *UsedApp, app *App, report bool, bearer_token *string, alert bool) *gin.H
}

type BaseErrorHandler struct {
	IErrorHandler
}

var ErrorHandler IErrorHandler

func InitBaseErrorHandler() *error {
	ErrorHandler = &BaseErrorHandler{}
	// Initialize gin engine
	ErrorHandler.Init()
	return nil
}

func (sm *BaseErrorHandler) Init() (err *error) {
	return nil
}

func (eh *BaseErrorHandler) ReportError(errorCode string, msg string, updateStatus bool, wf *Workflow, aWf *AWorkflow, report bool, bearer_token *string, alert bool) *gin.H {
	var returnMsg gin.H

	log.Println(msg)

	wf, aWf = eh.EnsureWorkflowInstances(errorCode, wf, aWf)

	// Update workflow status, if applicable
	if updateStatus {
		db, dbErr := GetDBConnection()
		if dbErr != nil {
			errorCode = STATUS_FAILED_TO_CONNECT_TO_DB
			msg = "Failed to connect to the db."
		} else {
			wf.ExternalId = aWf.Id
			wf.Name = aWf.Name
			wf.Status = errorCode
			wf.WorkspaceId = uuid.MustParse(Configuration.Workspace.Id)
			wf.Timestamp = time.Now()
			result := db.Save(&wf)
			if result.RowsAffected == 0 {
				errorCode = STATUS_FAILED_TO_SAVE
				msg = "Failed to save the status in the db."
			}
		}
	}

	returnMsg = gin.H{"Code": errorCode, "Message": msg}
	return &returnMsg
}

func (eh *BaseErrorHandler) EnsureWorkflowInstances(errorCode string, wf *Workflow, aWf *AWorkflow) (*Workflow, *AWorkflow) {
	if aWf == nil {
		var newAwf = AWorkflow{
			Id: 	-1,
			Name:	"AWorkflow has not been provided",
		}
		aWf = &newAwf
	}

	if wf == nil {
		var newWf = Workflow{
			ExternalId: aWf.Id,
			Name:       aWf.Name,
			Status:     errorCode,
		}
		wf = &newWf
	}
	return wf, aWf
}

func (eh *BaseErrorHandler) ReportAppError(errorCode string, msg string, uApp *UsedApp, app *App, report bool, bearer_token *string, alert bool) *gin.H {
	var returnMsg gin.H

	log.Println(msg)

	if uApp == nil {
		var projectId int64
		if app != nil {
			projectId = app.ProjectId
		}
		var ua = UsedApp{
			WorkflowId: projectId,
			Name:       app.Name,
			Status:     errorCode,
		}
		uApp = &ua
	}

	// No where to report for now.
	
	returnMsg = gin.H{"Code": errorCode, "Message": msg}
	return &returnMsg
}
