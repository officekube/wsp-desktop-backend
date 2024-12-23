package workspaceEngine

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	phttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Interface
type IAppApiController interface {
	InstallApp(c *gin.Context)
	UninstallApp(c *gin.Context)
	GetInstalledApps(c *gin.Context)
	ExecuteApp(c *gin.Context)
	StartApp(c *gin.Context)
	StopApp(c *gin.Context)

	Init(ginEngine *gin.Engine)	(error *error)
	CheckAppInstallIfNeeded(db *gorm.DB, app *App, personalToken string, bearerToken string, username string, status string) (*UsedApp, *gin.H)
}

type BaseAppApiController struct {
	IAppApiController
	RequestProcessedSuccessfully	bool
	Workflow 						*Workflow
	AWorkflow						*AWorkflow
	BearerToken						string
}

var AppApiController IAppApiController

func InitBaseAppApiController(ginEngine *gin.Engine) *error {
	AppApiController = &BaseAppApiController{}
	return AppApiController.Init(ginEngine)
}

func (aac *BaseAppApiController) Init(ginEngine *gin.Engine) (err *error) {
	if ginEngine == nil { 
		e := errors.New("A reference to the GIN engine cannot be nil.")
		err = &e
		return err
	}
	// 1. Populate routes that the monitor must serve
	var routes = Routes {
		{
			"InstalledAppsGet",
			http.MethodGet,
			"/api/apps",
			AppApiController.GetInstalledApps,
		},
		{
			"AppsInstallPost",
			http.MethodPost,
			"/api/apps/install",
			AppApiController.InstallApp,
		},
		{
			"AppsExecutePost",
			http.MethodPost,
			"/api/apps/execute",
			AppApiController.ExecuteApp,
		},
		{
			"AppsStartPost",
			http.MethodPost,
			"/api/apps/start",
			AppApiController.StartApp,
		},
		{
			"AppsStopPost",
			http.MethodPost,
			"/api/apps/stop",
			AppApiController.StopApp,
		},
		{
			"AppsUninstallPost",
			http.MethodPost,
			"/api/apps/uninstall",
			AppApiController.UninstallApp,
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

func (aac *BaseAppApiController) InstallApp(c *gin.Context) {
	aac.handleAppApiCall(c, "install", STATUS_FAILED_TO_INSTALL_APP)
}

func (aac *BaseAppApiController) UninstallApp(c *gin.Context) {
	// Cannot use the same methods used for install/start/stop
	//aac.handleAppApiCall(c, "uninstall", STATUS_FAILED_TO_INSTALL_APP)
	var err error
	// 0. Check authentication
	if SecurityManager.IsApiAuthenticated(c) > 0 {
		http.Error(c.Writer, "Failed to authenticate.", http.StatusUnauthorized)
		err = errors.New("failed to authenticate")
		return
	}

	bt := c.Request.Header.Get("Authorization")
	bearerToken := &bt

	// 1. Validate Input
	var app App
	if err := c.BindJSON(&app); err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportAppError(STATUS_INVALID_APP, "Bad App Object Format.",
			nil, &app, false, bearerToken, true))
		return
	}

	if app.Type != "installed" {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportAppError(STATUS_INVALID_APP, "The app can not be uninstalled as its type is NOT 'installed'",
			nil, &app, false, bearerToken, true))
		return
	}

	// 2. Extract personal token and username
	personalToken, tErr := AccountService.GetUserPlatformPersonalToken(*bearerToken)
	if tErr != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportAppError(STATUS_FAILED_TO_RETRIEVE_PERSONAL_TOKEN, "Failed to retrieve a user's personal token.",
				nil, &app, true, bearerToken, true))
		return
	}

	// 3. Get a DB connection
	db, err := GetDBConnection()
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportAppError(STATUS_FAILED_TO_CONNECT_TO_DB, "Failed to connect to internal database.", nil, &app, true, bearerToken, true))
		return
	}

	// 4. Execute uninstall.robot
	uApp := &UsedApp {
		WorkflowId: app.ProjectId,
		Name:            app.Name,
		Status:          STATUS_APP_UNINSTALLED,
		WorkspaceId:     uuid.MustParse(Configuration.Workspace.Id),
		Timestamp:       time.Now(),
	}
	aac.HandleApp(&app, uApp, db, personalToken, *bearerToken, "uninstall")
}

func (aac *BaseAppApiController) GetInstalledApps(c *gin.Context) {
	// The code implements a design outlined here https://workflow.officekube.io/platform/dwc-knowledge-base/-/blob/master/technology/wsp_apps.rst
	// 0. Check authentication
	if SecurityManager.IsApiAuthenticated(c) > 0 {
		http.Error(c.Writer, "Failed to authenticate.", http.StatusUnauthorized)
		return
	}

	db, err := GetDBConnection()
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportAppError(STATUS_FAILED_TO_CONNECT_TO_DB, "Failed to connect to internal database.", nil, nil, true, nil, true))
		return
	}

	var apps []App
	db.Table("apps").Preload(clause.Associations).Find(&apps)
	c.JSON(http.StatusOK, gin.H{"apps": apps})
}

func (aac *BaseAppApiController) ExecuteApp(c *gin.Context) {
	aac.handleAppApiCall(c, "execute", STATUS_FAILED_TO_EXECUTE_APP)
}

func (aac *BaseAppApiController) StartApp(c *gin.Context) {
	aac.handleAppApiCall(c, "start", STATUS_FAILED_TO_START_APP)
}

func (aac *BaseAppApiController) StopApp(c *gin.Context) {
	aac.handleAppApiCall(c, "stop", STATUS_FAILED_TO_STOP_APP)
}


func (aac *BaseAppApiController) handleAppApiCall(c *gin.Context, command string, failureCode string) {
	// 1-4. Preprocess the call
	err, db, app, uApp, bearerToken, personalToken := aac.preprocessCall(c)
	if err != nil {
		return
	}

	// 5. Install/execute/start/stop the app
	reportBytes, errorCode, outputPayload := aac.HandleApp(app, uApp, db, *personalToken, *bearerToken, command)

	// Return app output if it is available
	if errorCode != nil {
		if reportBytes != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Code": failureCode, "Message": string(reportBytes), "Output": outputPayload})
		} else {
			c.JSON(http.StatusBadRequest, errorCode)
		}
	} else {
		if reportBytes != nil {
			c.JSON(http.StatusOK, gin.H{"Code": STATUS_OK, "Message": string(reportBytes), "Output": outputPayload})
		} else {
			c.JSON(http.StatusOK, gin.H{"Code": STATUS_OK, "Message": "Successfully completed the command " + command + "for the app.", "Output": outputPayload})
		}
	}
}

func (aac *BaseAppApiController) preprocessCall(c *gin.Context) (err error, db *gorm.DB, app *App, uApp *UsedApp, bearerToken *string, personal_token *string) {
	// 0. Check authentication
	if SecurityManager.IsApiAuthenticated(c) > 0 {
		http.Error(c.Writer, "Failed to authenticate.", http.StatusUnauthorized)
		err = errors.New("failed to authenticate")
		return err, nil, nil, nil, nil, nil
	}

	bt := c.Request.Header.Get("Authorization")
	bearerToken = &bt

	// 1. Validate Input
	if err := c.BindJSON(&app); err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportAppError(STATUS_INVALID_APP, "Bad App Object Format.",
			nil, nil, false, bearerToken, true))
		return err, nil, nil, nil, bearerToken, nil
	}

	// 2. Extract personal token and username
	personalToken, tErr := AccountService.GetUserPlatformPersonalToken(*bearerToken)
	if tErr != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportAppError(STATUS_FAILED_TO_RETRIEVE_PERSONAL_TOKEN, "Failed to retrieve a user's personal token.",
				nil, app, true, bearerToken, true))
		err = *tErr
		return err, nil, app, nil, bearerToken, nil
	}
	
	loggedonUser := SecurityManager.GetLoggedOnUser(*bearerToken)

	// 3. Get a DB connection
	db, err = GetDBConnection()
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportAppError(STATUS_FAILED_TO_CONNECT_TO_DB, "Failed to connect to internal database.", nil, app, true, bearerToken, true))
		return err, nil, app, nil, bearerToken, &personalToken
	}
	
	uApp, h := aac.CheckAppInstallIfNeeded(db, app, personalToken, *bearerToken, loggedonUser.Username, STATUS_APP_TO_BE_INSTALLED)
	if uApp == nil {
		c.JSON(http.StatusBadRequest, h)
		err = errors.New("Failed to install the app.")
		return err, db, app, nil, bearerToken, &personalToken
	}

	return nil, db, app, uApp, bearerToken, &personalToken
}

func (aac *BaseAppApiController) CheckAppInstallIfNeeded(db *gorm.DB, app *App, personalToken string, bearerToken string, username string, status string) (*UsedApp, *gin.H) {
	var uApp *UsedApp
	// 1. Search if the passed in app has been already installed
	var installErr *error
	var eApp *App
	result := db.Table("apps").Where(clause.Eq{Column: "name", Value: app.Name}).Preload(clause.Associations).First(&eApp)
	if result.RowsAffected == 0 {
		// 2. If not, install it
		log.Println("No installed app found. Installing it...")
		uApp, installErr = aac.DoInstallApp(db, app, bearerToken, personalToken, username)
		if installErr != nil {
			return uApp, ErrorHandler.ReportAppError(STATUS_FAILED_TO_INSTALL_APP, "Failed to install the app:" + (*installErr).Error(), nil, app, true, &bearerToken, true)
		}
	} else {
		// We might still deal with a scenario where the app failed to install previously
		if eApp.Status == STATUS_FAILED_TO_INSTALL_APP {
			uApp, installErr = aac.DoInstallApp(db, app, bearerToken, personalToken, username)
			if installErr != nil {
				return uApp, ErrorHandler.ReportAppError(STATUS_FAILED_TO_INSTALL_APP, "Failed to install the app:" + (*installErr).Error(), nil, app, true, &bearerToken, true)
			}
		}
		// Make sure that the passed in app and its parameters have valid Ids. Also, do not allow changing any app props, only params' actual values can be changed.
		app.Id = eApp.Id
		app.DefaultBranch = eApp.DefaultBranch
		app.Description = eApp.Description
		app.HttpUrlToRepo = eApp.Description
		app.NameWithNamespace = eApp.NameWithNamespace
		app.Path = eApp.Path
		app.PathWithNamespace = eApp.PathWithNamespace
		app.ProjectId = eApp.ProjectId
		app.StartCount = eApp.StartCount
		app.Status = eApp.Status
		app.Topics = eApp.Topics
		app.Type = eApp.Type
		app.WebUrl = eApp.WebUrl
	

		validParamsCount := 0;
		for _, p := range app.Parameters {
			if p.Id >= 0 {
				validParamsCount++
			}
		}

		if validParamsCount != len(app.Parameters) {
			h := gin.H{"Code": STATUS_INVALID_APP, "Message": "At least one app parameter is not valid (does not have a correct Id)."}
			return  uApp, &h
		}

		h := gin.H{"Code": STATUS_OK, "Message": "The app is already installed."}

		uApp = &UsedApp {
			WorkflowId: app.ProjectId,
			Name:            app.Name,
			Status:          STATUS_APP_INSTALLED,
			WorkspaceId:     uuid.MustParse(Configuration.Workspace.Id),
			Timestamp:       time.Now(),
		}
		return  uApp, &h
	}

	// 3. For both persistent and ephemeral workspaces app dependencies might get installed outside the /work folder.
	// Ensure app dependencies are in place. 
	Util.CheckDependenciesInstallIfNeeded(filepath.Join(Configuration.App.InstallationFolder , app.Path))

	// 4. Notify the workflow manager about the app
	uApp.Status = status
	var err error
	if uApp.WorkspaceId, err = uuid.Parse(Configuration.Workspace.Id); err != nil {
		return uApp, ErrorHandler.ReportAppError(STATUS_FAILED_TO_PARSE_WORKSPACE_ID, "Failed to parse workspace Id.", uApp, app, true, &bearerToken, true)
	}
	// 5. Save the app
	// Make sure the type is populated
	if len(app.Type) == 0 {
		app.Type = "installed"
	}

	if app.Id == 0 {
		result = db.Create(&app)
	} else {
		result = db.Save(&app)
	}
	
	if result.RowsAffected == 0 {
		return uApp, ErrorHandler.ReportAppError(STATUS_FAILED_TO_SAVE, "Failed to save the status in the db.", nil, app, true, &bearerToken, true)
	}

	return uApp, nil
}

func (aac *BaseAppApiController) DoInstallApp(db *gorm.DB, app *App, bearerToken string, personalToken string, username string) (*UsedApp, *error) {
	var uApp UsedApp
	var err error

	// 1. Check out the repo into a temp folder
	repo, repoErr := git.PlainClone(filepath.Join(Configuration.App.InstallationFolder, app.Path), false, &git.CloneOptions{
		URL:      app.HttpUrlToRepo,
		Progress: os.Stdout,
		Auth:     &phttp.BasicAuth{Username: username, Password: personalToken},
		NoCheckout: false,
	})
	if repoErr != nil {
		err = repoErr
		return &uApp, &err
	}
	// Check out from the production branch
	// Get the working directory for the repository
	w, repoErr := repo.Worktree()
	if repoErr != nil {
		err = repoErr
		return &uApp, &err
	}

	// Find the right reference
	refs,_ := repo.References()
	var prodRef *plumbing.Reference
	refs.ForEach(func(ref *plumbing.Reference) error {
		if strings.Contains(string(ref.Name()), Configuration.App.ProductionBranch)  {
			prodRef = ref
			return nil
		}
		return nil
	})

	repoErr = w.Checkout(&git.CheckoutOptions{
		Branch: prodRef.Name(),
		Force:  true,
		Create: false,
	})
	if repoErr != nil {
		err = repoErr
		return &uApp, &err
	}

	// 2. Remove .git sub-folder
	if err = os.RemoveAll(filepath.Join(Configuration.App.InstallationFolder, app.Path, ".git")); err != nil {
		return &uApp, &err
	}

	// 3. Notify the workflow manager that the new app was installed
	uApp.WorkflowId = app.ProjectId
	uApp.Name = app.Name
	uApp.Status = STATUS_APP_INSTALLED
	uApp.WorkspaceId = uuid.MustParse(Configuration.Workspace.Id)
	uApp.Timestamp = time.Now()

	return &uApp, nil
}

func (aac *BaseAppApiController) HandleApp(app *App, uApp *UsedApp, db *gorm.DB, personalToken string, bearerToken string, operation string) ([]byte, *gin.H, map[string]interface{}) {
	var reportBytes []byte
	var outputPayload map[string]interface{}
	var errorCode *gin.H
	// 6.1 Check what workflow type it is (app)
	typeTopic := WorkflowApiController.FindTopic(app.Topics, "type")
	if typeTopic == nil {
		errorCode = ErrorHandler.ReportAppError(STATUS_WORKFLOW_TYPE_IS_MISSING, "The workflow type is missing.",
			uApp, app, false, &bearerToken, true)
		return reportBytes, errorCode, outputPayload
	}

	// 6.2 Check what target is
	targetTopic := WorkflowApiController.FindTopic(app.Topics, "target")
	if targetTopic == nil {
		errorCode = ErrorHandler.ReportAppError(STATUS_WORKFLOW_TARGET_IS_MISSING, "The workflow target is missing.",
			uApp, app, false, &bearerToken, true)
		return reportBytes, errorCode, outputPayload
	}

	statusCode := STATUS_APP_STARTED

	switch operation {
		case "start":
			statusCode = STATUS_APP_STARTED
		case "stop":
			statusCode = STATUS_APP_STOPPED
		case "install":
			statusCode = STATUS_APP_INSTALLED
		case "execute":
			statusCode = STATUS_APP_EXECUTED
		case "uninstall":
			statusCode = STATUS_APP_UNINSTALLED
		default:
			statusCode = STATUS_APP_STARTED
	}

	workflowType := strings.Split(*typeTopic, "=")[1]
	workflowTarget := strings.Split(*targetTopic, "=")[1]

	// 7.1 Handle the command
	if workflowTarget == "workspace" {
		if workflowType == "app" {
			// https://pkg.go.dev/os/exec#Command
			robotFile := operation + ".robot"
			taskDir := filepath.Join(Configuration.App.InstallationFolder, app.Path)

			// Odd way to format, as per https://go.dev/src/time/format.go
			var nameSuffix = time.Now().Format("20060102_150405")
			var fileName = "variables_" + nameSuffix
			// If there are project parameters dump them into a file variables_yyyymmdd_hhmmss.yml and pass it into the command line.
			var paramsAreThere = aac.checkParameters(app, fileName, taskDir)

			taskPath := filepath.Join(taskDir, robotFile)
			varPath := filepath.Join(taskDir, fileName + ".yml")
			varLog := filepath.Join(taskDir, "log_" + nameSuffix + ".html")
			varReport := filepath.Join(taskDir, "report_" + nameSuffix + ".html")
			var cmd *exec.Cmd
			if paramsAreThere {
				log.Println("The app has variable(s) defined in file: ", fileName + ".yml")
				cmd = exec.Command("robot", "-V", varPath, "-l", varLog, "-r", varReport, taskPath)
			} else {
				cmd = exec.Command("robot", "-l", varLog, "-r", varReport, taskPath)
			}
			cmd.Env = os.Environ()

			envs := aac.addEnvironmentVariables(app, bearerToken, personalToken)
			cmd.Env = append(cmd.Env, envs[:]...)
			
			cmd.Dir = taskDir
			err := cmd.Run()
			// Move report, variables, and log to the tasklogs folder
			if _, err := os.Stat(filepath.Join(Configuration.App.InstallationFolder, "logs")); errors.Is(err, os.ErrNotExist) {
				os.Mkdir(filepath.Join(Configuration.App.InstallationFolder, "logs"), os.ModePerm)
			}
			os.Rename(varLog, filepath.Join(Configuration.App.InstallationFolder, "logs", "log_"+nameSuffix+".html"))
			os.Rename(varReport, filepath.Join(Configuration.App.InstallationFolder,"logs", "report_"+nameSuffix+".html"))
			os.Rename(varPath, filepath.Join(Configuration.App.InstallationFolder, "logs", fileName+".yml"))
			os.Remove(filepath.Join(taskDir, "output.xml"))
			// Construct a URL to the report html file
			reportUrl := "https://" + Configuration.Workspace.Domain + "/applogs/report_" + nameSuffix + ".html"
			reportBytes = []byte(reportUrl)

			errMsg := "Failed to " +  operation + " the app"

			if err != nil {

				if exiterr, ok := err.(*exec.ExitError); ok {
					// The program has exited with an exit code != 0

					// This works on both Unix and Windows. Although package
					// syscall is generally platform dependent, WaitStatus is
					// defined for both Unix and Windows and in both cases has
					// an ExitStatus() method with the same signature.
					if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
						statusCode = "Failed to " + operation
						errMsg +=  " with status code: " + fmt.Sprint(status.ExitStatus()) + ". Report: " + reportUrl
						errorCode = ErrorHandler.ReportAppError(statusCode, errMsg,
							uApp, app, true, &bearerToken, true)
					}
				} else {
					statusCode = "Failed to " + operation
					errMsg +=  ". Report: " + reportUrl
					errorCode = ErrorHandler.ReportAppError(statusCode, errMsg, uApp, app, true, &bearerToken, true)
				}
			}

			// Check if the app has produced the file output.json
			outputFile, err := os.ReadFile(filepath.Join(taskDir, "output.json"))
			if err == nil {
				json.Unmarshal(outputFile, &outputPayload)
				// Remove the output
				os.Remove(filepath.Join(taskDir, "output.json"))
			}
		} else {
			statusCode = STATUS_WORKFLOW_TYPE_IS_NOT_SUPPORTED
			errorCode = ErrorHandler.ReportAppError(statusCode, "The workflow type" + workflowType + " is NOT supported.",
				uApp, app, false, &bearerToken, true)
		}
	} else {
		statusCode = STATUS_WORKFLOW_TARGET_IS_NOT_SUPPORTED
		errorCode = ErrorHandler.ReportAppError(statusCode, "The workflow target" + workflowTarget + " is NOT supported.",
			uApp, app, false, &bearerToken, true)
	}

	// 7.1. Update the app status
	app.Status = statusCode
	var result *gorm.DB
	if operation == "install" || operation == "start" || operation == "stop" {
		if app.Id == 0 {
			result = db.Create(&app)
		} else {
				result = db.Save(&app)
		}
	} else {
		// We should delete the record from the db only if the uninstall command succeeded
		if statusCode == STATUS_APP_UNINSTALLED {
			result = db.Where("project_id = ?", app.ProjectId).Delete(&app)
		}
	}
	
	if result != nil && result.RowsAffected == 0 {
		errorCode = ErrorHandler.ReportAppError(STATUS_FAILED_TO_SAVE, "Failed to save the status in the db.", nil, app, true, &bearerToken, true)
	}

	// 8. Notify the workflow manager about the status of the workflow execution
	return reportBytes, errorCode, outputPayload
}

func (aac *BaseAppApiController) addEnvironmentVariables(app *App, bearerToken string, personalToken string) (envs []string) {

	// 1. Add Domain and workspace id variables
	envs = append(envs, "ok_platform_domain=" + Configuration.PlatformDomain)
	envs = append(envs, "workspace_id=" + Configuration.Workspace.Id)

	// 2. Add token to the environment variables, if it is included in the parameters
	var tokenParam AppParameter
	var username string
	for _, p := range app.Parameters {
		if p.Name == "token" {
			tokenParam = p
			loggedonUser := SecurityManager.GetLoggedOnUser(bearerToken)
			username = loggedonUser.Username
			break
		}
	}
	if len(app.Parameters) > 0 && len(tokenParam.ActualValues) > 0 {
		var extractedToken string
		// Multiple tokens can be set.
		for _, v := range tokenParam.ActualValues {
			if v == "ok_access_token" {
				extractedToken = personalToken
			} else if v == "ok_bearerToken" {
				extractedToken = strings.Split(bearerToken, " ")[1]
			} else {
				// Pull the IdP issued token
				extractedToken = SecurityManager.GetIdPToken(bearerToken, v)
			}
			envs = append(envs, v + "=" + extractedToken)
		}
		envs = append(envs, "username=" + username)
	}

	// 3. Add app environment variables
	for _, e := range Configuration.AppEnvironmentVariables {
		envs = append(envs, e.Name + "=" + e.Value)
	}
	return envs
}

func (aac *BaseAppApiController) checkParameters(app *App, fileName string, path string) bool {
	var result bool = true

	paramViper := viper.New()

	for _, param := range app.Parameters {
		// Skip reserved parameters.
		if param.Name == "token" {
			continue
		}
		paramViper.Set(param.Name, strings.Join(param.ActualValues, ", "))
	}

	// Add workflow variables ok_platform_domain, robots_folder, and workspace_id
	paramViper.Set("ok_platform_domain", Configuration.PlatformDomain)
	paramViper.Set("apps_folder", "/work/apps")
	paramViper.Set("workspace_id", Configuration.Workspace.Id)

	if err := paramViper.WriteConfigAs(filepath.Join(path, fileName) + ".yml"); err != nil {
		log.Println("Failed to save the variables file...")
		result = false
	}

	return result
}