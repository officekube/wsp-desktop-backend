package workspaceEngine

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"
)

// Interface
type IEngineApiController interface {
	Init(ginEngine *gin.Engine) (error *error)
	GetConfig(c *gin.Context)
	RunStartupTasks(c *gin.Context)
}

type BaseEngineApiController struct {
	IEngineApiController
}

var EngineApiController IEngineApiController

func InitBaseEngineApiController(ginEngine *gin.Engine) *error {
	EngineApiController = &BaseEngineApiController{}
	return EngineApiController.Init(ginEngine)
}

func (eac *BaseEngineApiController) Init(ginEngine *gin.Engine) (err *error) {
	if ginEngine == nil {
		e := errors.New("A reference to the GIN engine cannot be nil.")
		err = &e
		return err
	}
	// 1. Populate routes that the controller must serve
	var routes = Routes{
		{
			"EngineConfigGet",
			http.MethodGet,
			"/api/engine/config",
			EngineApiController.GetConfig,
		},
		{
			"EngineRunStartupTasksGet",
			http.MethodGet,
			"/api/engine/runStartupTasks",
			EngineApiController.RunStartupTasks,
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

	return nil
}


// The method returns a configuration to be used by the engine's frontend.
func (eac *BaseEngineApiController) GetConfig(c *gin.Context) {
	// 0. Check authentication
	if SecurityManager.IsApiAuthenticated(c) > 0 {
		http.Error(c.Writer, "Failed to authenticate.", http.StatusUnauthorized)
		return
	}

	// 1. Convert the workspace config into json
	if Configuration != nil {
		c.JSON(http.StatusOK, Configuration.Frontend )
		// 2. Update the FirstTimeLaunched setting to true
		if(!Configuration.Frontend.FirstTimeLaunched) {
			ConfigMgr.UpdateWorkspaceConfig("frontend.firstTimeLaunched", true)
		}
	} else {
		c.JSON(http.StatusNotFound, gin.H{"message": "No config was found."})
	}
}

// The method executes all startup tasks.
func (eac *BaseEngineApiController) RunStartupTasks(c *gin.Context) {
	// 0. Check authentication
	if SecurityManager.IsApiAuthenticated(c) > 0 {
		http.Error(c.Writer, "Failed to authenticate.", http.StatusUnauthorized)
		return
	}

	bearer_token := c.Request.Header.Get("Authorization")

	// 1. Check if the tasks have already been executed.
	if Configuration.Workspace.FirstTimeLaunched == 2 {
		c.JSON(http.StatusOK, gin.H{"Code": STATUS_OK, "Message": "Tasks have already been executed at startup time."})
		return
	}

	// 2. Pull all records from the workflow table that have at least one record in the workflowschedule table with the field Start set to true.
	db, err := GetDBConnection()
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_CONNECT_TO_DB, "Failed to connect to internal database.", false, nil, nil, true, &bearer_token, true))
		return
	}

	var schedules []WorkflowSchedule
	result := db.Table("workflow_schedules").Where(clause.Eq{Column: "Start", Value: true}).Find(&schedules)
	if result.RowsAffected == 0 {
		ConfigMgr.UpdateWorkspaceConfig("workspace.firstTimeLaunched", 2)
		c.JSON(http.StatusOK, gin.H{"Code": STATUS_OK, "Message": "No tasks to run at startup time."})
		return
	}

	var workflow *Workflow
	var errorCode *gin.H
	var reportBytes []byte
	var outputPayload map[string]interface{}
	var tskErrorCode *gin.H
	var tskReportBytes []byte
	var tskOutputPayload map[string]interface{}

	personalToken, tErr := AccountService.GetUserPlatformPersonalToken(bearer_token)
	if tErr != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_RETRIEVE_PERSONAL_TOKEN, "Failed to retrieve a user's personal token.",
			false, nil, nil, true, &bearer_token, true))
		return
	}

	for _, schedule := range schedules {
		// 2. Execute each workflow
		result = db.Table("workflows").Where(clause.Eq{Column: "Id", Value: schedule.WorkflowId}).Find(&workflow)
		if result.RowsAffected == 1 {
			// Get a relevant git project
			aWf := PlatformRepo.GetPlatformRepo(workflow, personalToken)
			if aWf != nil {
				tskReportBytes, tskErrorCode, tskOutputPayload = WorkflowApiController.ExecuteWorkflow(aWf, workflow, db, personalToken, c.Request.Header.Get("Authorization"))
				if tskErrorCode != nil {
					// If the task failed we will keep going but report last failed task.
					reportBytes = tskReportBytes
					errorCode = tskErrorCode
					outputPayload = tskOutputPayload
				}
			}
		}
	}

	if errorCode != nil {
		if reportBytes != nil {
			c.Data(http.StatusBadRequest, "text/html", reportBytes)
		} else {
			c.JSON(http.StatusBadRequest, errorCode)
		}
	} else {
		if reportBytes != nil {
			c.JSON(http.StatusOK, gin.H{"Code": STATUS_OK, "Message": reportBytes, "Output": outputPayload})
		} else {
			c.JSON(http.StatusOK, gin.H{"Code": STATUS_OK, "Message": "All tasks have been successfully executed at the start of the workspace.", "Output": outputPayload})
		}
	}
	ConfigMgr.UpdateWorkspaceConfig("workspace.firstTimeLaunched", 2)
}


