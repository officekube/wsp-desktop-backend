package workspaceEngine

import (
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gomarkdown/markdown"
	"github.com/google/uuid"

	"encoding/json"
	"errors"
	"fmt"

	"strconv"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	phttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/jinzhu/copier"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Interface
type IWorkflowApiController interface {
	WorkflowsExecutePost(c *gin.Context)
	WorkflowsViewPost(c *gin.Context)
	GetWorkflowEstimate(c *gin.Context)
	Init(ginEngine *gin.Engine) (error *error)
	ExecuteWorkflow(aWf *AWorkflow, workflow *Workflow, db *gorm.DB, personalToken string, bearerToken string) ([]byte, *gin.H, map[string]interface{})
	CheckWorkflowInstallIfNeeded(db *gorm.DB, aWf AWorkflow, personalToken string, bearerToken string, username string, status string) (*Workflow, *gin.H)
	FindTopic(topics []string, searchTerm string) *string
	InstallWorkflow(aWf *AWorkflow, bearerToken string, personalToken string, username string) (*Workflow, *error)
	WorkflowsSchedulePost(c *gin.Context)
	ScheduleWorkflow(db *gorm.DB, aWorkflow AWorkflow, bearer_token string) string
	WorkflowsHistoryGet(c *gin.Context)
}

type BaseWorkflowApiController struct {
	IWorkflowApiController
	RequestProcessedSuccessfully bool
	Workflow                     *Workflow
	AWorkflow                    *AWorkflow
	BearerToken                  string
}

var WorkflowApiController IWorkflowApiController

func InitBaseWorkflowApiController(ginEngine *gin.Engine) *error {
	WorkflowApiController = &BaseWorkflowApiController{}
	return WorkflowApiController.Init(ginEngine)
}

func (wac *BaseWorkflowApiController) Init(ginEngine *gin.Engine) (err *error) {
	if ginEngine == nil {
		e := errors.New("A reference to the GIN engine cannot be nil.")
		err = &e
		return err
	}
	// 1. Populate routes that the controller must serve
	var routes = Routes{
		{
			"WorkflowsExecutePost",
			http.MethodPost,
			"/api/workflows/execute",
			WorkflowApiController.WorkflowsExecutePost,
		},
		{
			"WorkflowsViewPost",
			http.MethodPost,
			"/api/workflows/view",
			WorkflowApiController.WorkflowsViewPost,
		},
		{
			"WorkflowsEstimatePost",
			http.MethodPost,
			"/api/workflows/estimate",
			WorkflowApiController.GetWorkflowEstimate,
		},
		{
			"WorkflowsSchedulePost",
			http.MethodPost,
			"/api/workflows/schedule",
			WorkflowApiController.WorkflowsSchedulePost,
		},
		{
			"WorkflowsHistoryGet",
			http.MethodGet,
			"/api/workflows/history",
			WorkflowApiController.WorkflowsHistoryGet,
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

// WorkflowsExecutePost - The method executes passed in workflow.
func (wac *BaseWorkflowApiController) WorkflowsExecutePost(c *gin.Context) {
	// The code implements a design outlined here https://workflow.officekube.io/platform/dwc-knowledge-base/-/blob/master/workflow.md
	// 0. Check authentication
	if SecurityManager.IsApiAuthenticated(c) > 0 {
		http.Error(c.Writer, "Failed to authenticate.", http.StatusUnauthorized)
		return
	}

	bearerToken := c.Request.Header.Get("Authorization")

	// 1. Parse out the data to an instance of the workflow model
	var aWf AWorkflow
	if err := c.BindJSON(&aWf); err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_INVALID_AWORKFLOW, "Bad AWorkflow Object Format.",
			false, nil, nil, false, &bearerToken, true))
		return
	}

	// 2 - 4
	db, err := GetDBConnection()
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_CONNECT_TO_DB, "Failed to connect to internal database.", false, nil, nil, true, &bearerToken, true))
		return
	}

	personalToken, tErr := AccountService.GetUserPlatformPersonalToken(bearerToken)
	if tErr != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_RETRIEVE_PERSONAL_TOKEN, "Failed to retrieve a user's personal token.",
			false, nil, &aWf, true, &bearerToken, true))
		return
	}

	loggedonUser := SecurityManager.GetLoggedOnUser(bearerToken)
	workflow, jsErr := wac.CheckWorkflowInstallIfNeeded(db, aWf, personalToken, bearerToken, loggedonUser.Username, STATUS_TO_BE_EXECUTED)
	if jsErr != nil {
		c.JSON(http.StatusBadRequest, jsErr)
		return
	}

	// 6-8
	reportBytes, errorCode, outputPayload := wac.ExecuteWorkflow(&aWf, workflow, db, personalToken, bearerToken)

	// Return task output if it is available
	if errorCode != nil {
		if reportBytes != nil {
			c.JSON(http.StatusBadRequest, gin.H{"Code": STATUS_FAILED_TO_EXECUTE_TASK, "Message": string(reportBytes), "Output": outputPayload})
		} else {
			c.JSON(http.StatusBadRequest, errorCode)
		}
	} else {
		if reportBytes != nil {
			c.JSON(http.StatusOK, gin.H{"Code": STATUS_OK, "Message": string(reportBytes), "Output": outputPayload})
		} else {
			c.JSON(http.StatusOK, gin.H{"Code": STATUS_OK, "Message": "Successfully executed the task.", "Output": outputPayload})
		}
	}
}

func (wac *BaseWorkflowApiController) ExecuteWorkflow(aWf *AWorkflow, workflow *Workflow, db *gorm.DB, personalToken string, bearerToken string) ([]byte, *gin.H, map[string]interface{}) {
	var reportBytes []byte
	var outputPayload map[string]interface{}
	var errorCode *gin.H
	// 6. Execute the workflow
	// 6.1 Check what workflow type it is (task/library/workflow)
	typeTopic := wac.FindTopic(aWf.Topics, "type")
	if typeTopic == nil {
		errorCode = ErrorHandler.ReportError(STATUS_WORKFLOW_TYPE_IS_MISSING, "The workflow type is missing.",
			true, workflow, aWf, false, &bearerToken, true)
		return reportBytes, errorCode, outputPayload
	}

	// 6.2 Check what target is
	targetTopic := wac.FindTopic(aWf.Topics, "target")
	if targetTopic == nil {
		errorCode = ErrorHandler.ReportError(STATUS_WORKFLOW_TARGET_IS_MISSING, "The workflow target is missing.",
			true, workflow, aWf, false, &bearerToken, true)
		return reportBytes, errorCode, outputPayload
	}

	statusCode := STATUS_EXECUTED
	workflowType := strings.Split(*typeTopic, "=")[1]
	workflowTarget := strings.Split(*targetTopic, "=")[1]

	if workflowTarget == "workspace" {
		if workflowType == "task" || workflowType == "prompt" {
			// https://pkg.go.dev/os/exec#Command
			robotFile := "task.robot"
			taskDir := filepath.Join(Configuration.Workflow.InstallationFolder, aWf.Path)

			// Odd way to format, as per https://go.dev/src/time/format.go
			var nameSuffix = time.Now().Format("20060102_150405")
			var fileName = "variables_" + nameSuffix
			// If there are project parameters dump them into a file variables_yyyymmdd_hhmmss.yml and pass it into the command line.
			var paramsAreThere = wac.addProjectParametersAsTaskEnvironmentVariables(*aWf, fileName, taskDir)

			taskPath := filepath.Join(taskDir, robotFile)
			varPath := filepath.Join(taskDir, fileName + ".yml")
			varLog := filepath.Join(taskDir, "log_" + nameSuffix + ".html")
			varReport := filepath.Join(taskDir, "report_" + nameSuffix + ".html")
			var cmd *exec.Cmd
			if paramsAreThere {
				log.Println("The task has variable(s) defined in file: ", fileName+".yml")
				cmd = exec.Command("robot", "-V", varPath, "-l", varLog, "-r", varReport, taskPath)
			} else {
				cmd = exec.Command("robot", "-l", varLog, "-r", varReport, taskPath)
			}
			cmd.Env = os.Environ()

			envs := wac.addEnvironmentVariables(aWf, bearerToken, personalToken)
			cmd.Env = append(cmd.Env, envs[:]...)

			// Save std err and out to a file. This for troubleshooting
			/*
				execLogFile, _ := os.OpenFile("exec.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)

				mw := io.MultiWriter(os.Stdout, execLogFile)
				cmd.Stdout = mw
				cmd.Stderr = mw
			*/
			cmd.Dir = taskDir
			err := cmd.Run()
			// Move report, variables, and log to the tasklogs folder
			if _, err := os.Stat(filepath.Join(Configuration.Workflow.InstallationFolder, "logs")); errors.Is(err, os.ErrNotExist) {
				os.Mkdir(filepath.Join(Configuration.Workflow.InstallationFolder, "logs"), os.ModePerm)
			}
			os.Rename(varLog, filepath.Join(Configuration.Workflow.InstallationFolder, "logs", "log_"+nameSuffix+".html"))
			os.Rename(varReport, filepath.Join(Configuration.Workflow.InstallationFolder, "logs", "report_"+nameSuffix+".html"))
			os.Rename(varPath, filepath.Join(Configuration.Workflow.InstallationFolder, "logs", fileName+".yml"))
			os.Remove(filepath.Join(taskDir, "variables.json"))
			os.Remove(filepath.Join(taskDir, "output.xml"))
			// Construct a URL to the report html file
			reportUrl := "https://" + Configuration.Workspace.Domain + "/tasklogs/report_" + nameSuffix + ".html"
			reportBytes = []byte(reportUrl)

			errMsg := "Failed to execute the task"

			if err != nil {

				if exiterr, ok := err.(*exec.ExitError); ok {
					// The program has exited with an exit code != 0

					// This works on both Unix and Windows. Although package syscall is generally platform dependent,
					// WaitStatus is defined for both Unix and Windows and in both cases has
					// an ExitStatus() method with the same signature.
					if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
						statusCode = STATUS_FAILED_TO_EXECUTE_TASK
						errMsg += " with status code: " + fmt.Sprint(status.ExitStatus()) + ". Report: " + reportUrl
						errorCode = ErrorHandler.ReportError(statusCode, errMsg,
							true, workflow, aWf, true, &bearerToken, true)
					}
				} else {
					statusCode = STATUS_FAILED_TO_EXECUTE_TASK
					errMsg += ". Report: " + reportUrl
					errorCode = ErrorHandler.ReportError(statusCode, errMsg, true, workflow, aWf, true, &bearerToken, true)
				}
			}

			// Check if the task has produced the file output.json
			outputFile, err := os.ReadFile(filepath.Join(taskDir, "output.json"))
			if err == nil {
				json.Unmarshal(outputFile, &outputPayload)
				// Remove the output
				os.Remove(filepath.Join(taskDir, "/output.json"))
			}
		} else {
			statusCode = STATUS_WORKFLOW_TYPE_IS_NOT_SUPPORTED
			errorCode = ErrorHandler.ReportError(statusCode, "The workflow type"+workflowType+" is NOT supported.",
				true, workflow, aWf, false, &bearerToken, true)
		}
	} else {
		statusCode = STATUS_WORKFLOW_TARGET_IS_NOT_SUPPORTED
		errorCode = ErrorHandler.ReportError(statusCode, "The workflow target"+workflowTarget+" is NOT supported.",
			true, workflow, aWf, false, &bearerToken, true)
	}

	// 7. Store the status in the db
	workflow.ExternalId = aWf.Id
	workflow.Name = aWf.Name
	workflow.Status = statusCode
	workflow.WorkspaceId = uuid.MustParse(Configuration.Workspace.Id)
	workflow.Timestamp = time.Now()

	result := db.Save(&workflow)
	if result.RowsAffected == 0 {
		statusCode = STATUS_FAILED_TO_SAVE
		errorCode = ErrorHandler.ReportError(statusCode, "Failed to save the status in the db.", false, nil, nil, true, &bearerToken, true)
	} else {
		wac.updateParametersIfNeeded(workflow, aWf.Parameters, db)
	}

	return reportBytes, errorCode, outputPayload
}

func (wac *BaseWorkflowApiController) addEnvironmentVariables(aWf *AWorkflow, bearerToken string, personalToken string) (envs []string) {

	// 1. Add Domain and workspace id variables
	envs = append(envs, "ok_platform_domain="+Configuration.PlatformDomain)
	envs = append(envs, "workspace_id="+Configuration.Workspace.Id)

	// 2. Add token to the environment variables, if it is included in the parameters
	var tokenParam AWorkflowParameter
	var username string
	for _, p := range aWf.Parameters {
		if p.Name == "token" {
			tokenParam = p
			loggedonUser := SecurityManager.GetLoggedOnUser(bearerToken)
			username = loggedonUser.Username
			break
		}
	}
	if len(aWf.Parameters) > 0 && len(tokenParam.ActualValues) > 0 {
		var extractedToken string
		// Multiple tokens can be set.
		for _, v := range tokenParam.ActualValues {
			if v == "ok_access_token" {
				extractedToken = personalToken
			} else if v == "ok_bearerToken" {
				extractedToken = strings.Split(bearerToken, " ")[1]
			} else {
				// Pull the IdP issued token from keycloak (as per https://workflow.officekube.io/platform/dwc-knowledge-base/-/blob/master/technology/external_idp.md)
				extractedToken = SecurityManager.GetIdPToken(bearerToken, v)
			}
			envs = append(envs, v+"="+extractedToken)
		}
		envs = append(envs, "username="+username)
	}

	// 3. Add workflow environment variables loaded from the workspace configuration
	for _, e := range Configuration.WorkflowEnvironmentVariables {
		envs = append(envs, e.Name+"="+e.Value)
	}
	return envs
}

func (wac *BaseWorkflowApiController) addProjectParametersAsTaskEnvironmentVariables(aWf AWorkflow, fileName string, path string) bool {
	var result bool = true

	paramViper := viper.New()

	for _, param := range aWf.Parameters {
		// Skip reserved parameters.
		if param.Name == "token" {
			continue
		}
		paramViper.Set(param.Name, strings.Join(param.ActualValues, ", "))
	}

	// Add workflow variables ok_platform_domain, robots_folder, and workspace_id
	paramViper.Set("ok_platform_domain", Configuration.PlatformDomain)
	paramViper.Set("apps_folder", Configuration.App.InstallationFolder)
	paramViper.Set("workspace_id", Configuration.Workspace.Id)

	if err := paramViper.WriteConfigAs(filepath.Join(path, fileName) + ".yml"); err != nil {
		log.Println("Failed to save the variables file...")
		result = false
	}

	// Also save the config as a json file to be consumed by the task if needed
	paramViper.SetConfigType("json")
	if err := paramViper.WriteConfigAs(path + "/variables.json"); err != nil {
		log.Println("Failed to save the variables file as a json file...")
		result = false
	}

	return result
}

func (wac *BaseWorkflowApiController) CheckWorkflowInstallIfNeeded(db *gorm.DB, aWf AWorkflow, personalToken string, bearerToken string, username string, status string) (*Workflow, *gin.H) {
	var workflow *Workflow
	// 1. Search if the passed in workflow has been already installed

	var installErr *error
	//Troubleshoot workflowSchema, _ := schema.Parse(Workflow{}, &sync.Map{}, schema.NamingStrategy{})
	//Troubleshoot _ = workflowSchema
	result := db.Table("workflows").Where(clause.Eq{Column: "external_id", Value: aWf.Id}).First(&workflow)
	if result.RowsAffected == 0 {
		// 2. If not, install it
		log.Println("No installed workflow found. Installing it...")
		workflow, installErr = wac.InstallWorkflow(&aWf, bearerToken, personalToken, username)
		if installErr != nil {
			return workflow, ErrorHandler.ReportError(STATUS_FAILED_TO_INSTALL_WORKFLOW, "Failed to install the workflow: "+(*installErr).Error(), false, workflow, &aWf, true, &bearerToken, true)
		}
	}

	// 3. For both persistent and ephemeral workspaces workflow dependencies might get installed outside the /work folder.
	// Ensure workflow dependencies are in place.
	Util.CheckDependenciesInstallIfNeeded(filepath.Join(Configuration.Workflow.InstallationFolder, aWf.Path))

	// 4. Notify the workflow manager about the workflow
	workflow.Status = status
	var err error
	if workflow.WorkspaceId, err = uuid.Parse(Configuration.Workspace.Id); err != nil {
		return workflow, ErrorHandler.ReportError(STATUS_FAILED_TO_PARSE_WORKSPACE_ID, "Failed to parse workspace Id.", true, workflow, &aWf, true, &bearerToken, true)
	}

	return workflow, nil
}

func (wac *BaseWorkflowApiController) InstallWorkflow(aWf *AWorkflow, bearerToken string, personalToken string, username string) (*Workflow, *error) {
	var workflow Workflow
	var err error

	// 1. Check out the repo into a temp folder
	// https://github.com/go-git/go-git
	// This is based on the sample github.com/go-git/go-git/v5@v5.4.2/_examples/clone/auth/basic/access_token
	repo, repoErr := git.PlainClone(filepath.Join(Configuration.Workflow.InstallationFolder, aWf.Path), false, &git.CloneOptions{
		URL:        aWf.HttpUrlToRepo,
		Progress:   os.Stdout,
		Auth:       &phttp.BasicAuth{Username: username, Password: personalToken},
		NoCheckout: false,
	})
	if repoErr != nil {
		// Check if the error == "repository already exists"
		if repoErr.Error() ==  "repository already exists" {
			// Return successfully
			return wac.createWorkflow(aWf)
		}
		err = repoErr
		return &workflow, &err
	}

	// Check out from the production branch
	// Get the working directory for the repository
	w, repoErr := repo.Worktree()
	if repoErr != nil {
		err = repoErr
		return &workflow, &err
	}

	// Find the right reference
	refs, _ := repo.References()
	var prodRef *plumbing.Reference
	refs.ForEach(func(ref *plumbing.Reference) error {
		if strings.Contains(string(ref.Name()), Configuration.Workflow.ProductionBranch) {
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
		return &workflow, &err
	}

	// 2. Remove .git sub-folder
	if err = os.RemoveAll(filepath.Join(Configuration.Workflow.InstallationFolder, aWf.Path, ".git")); err != nil {
		return &workflow, &err
	}
	// 3. Add a record to the local db about successful installation
	return wac.createWorkflow(aWf)
}

func (wac *BaseWorkflowApiController) createWorkflow(aWf *AWorkflow) (workflow *Workflow, err *error) {
	db, dbErr := GetDBConnection()

	if dbErr != nil {
		err = &dbErr
		return workflow, err
	}
	workflow = &Workflow{}
	workflow.ExternalId = aWf.Id
	workflow.Name = aWf.Name
	workflow.Status = STATUS_INSTALLED
	workflow.WorkspaceId = uuid.MustParse(Configuration.Workspace.Id)
	workflow.Id = uuid.New()
	workflow.Timestamp = time.Now()
	workflow.Path = aWf.Path
	workflow.Type = aWf.Type
	workflow.HttpUrlToRepo = aWf.HttpUrlToRepo

	result := db.Create(&workflow)
	if result.RowsAffected == 0 {
		err2 := errors.New("Could not add the workflow record.")
		err = &err2
		return workflow, err
	}
	return workflow, nil
}

func (wac *BaseWorkflowApiController) WorkflowsViewPost(c *gin.Context) {
	wac.RequestProcessedSuccessfully = false
	// 0. Check authentication
	if SecurityManager.IsApiAuthenticated(c) > 0 {
		http.Error(c.Writer, "Failed to authenticate.", http.StatusUnauthorized)
		return
	}

	// 1. Parse out the data to an instance of the used workflow model
	bearerToken := c.Request.Header.Get("Authorization")
	wac.BearerToken = bearerToken
	var uWorkflow UsedWorkflow
	if err := c.BindJSON(&uWorkflow); err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_INVALID_USED_WORKFLOW, "Bad UsedWorkflow Object Format.",
			false, nil, nil, false, &bearerToken, true))
		return
	}
	wac.AWorkflow = &uWorkflow.Aworkflow

	// 2. Report to the workflow manager that the workflow was viewed.
	uWorkflow.Status = "WORKFLOW_VIEWED"
	var workflow Workflow
	workflow.ExternalId = uWorkflow.Aworkflow.Id
	workflow.Name = uWorkflow.Aworkflow.Name
	workflow.Status = uWorkflow.Status
	workflow.WorkspaceId = uuid.MustParse(Configuration.Workspace.Id)
	workflow.Timestamp = time.Now()

	wac.Workflow = &workflow

	// 3. Check what type a passed aworkflow instance is.
	typeTopic := wac.FindTopic(uWorkflow.Aworkflow.Topics, "type")
	workflowType := "task"
	if typeTopic != nil {
		workflowType = strings.Split(*typeTopic, "=")[1]
	}

	if workflowType == "task" {
		// 2. Pull readme.rst or readme.md (if .rst does not exist) from the platform repo.
		// Check README.rst in the platform-workflows group first
		readmeType := 1 // rst
		prjId := int(uWorkflow.Aworkflow.Id)
		content, errCode := PlatformRepo.GetRepoFileContent(Configuration.Gitlab.PlatformWorkflowsToken, prjId, "README.rst")
		if errCode > 0 {
			readmeType = 2 // md
			content, errCode = PlatformRepo.GetRepoFileContent(Configuration.Gitlab.PlatformWorkflowsToken, prjId, "README.md")
			if errCode > 0 {
				// Now try the user-workflows group
				readmeType = 1
				content, errCode = PlatformRepo.GetRepoFileContent(Configuration.Gitlab.UserWorkflowsToken, prjId, "README.rst")
				if errCode > 0 {
					readmeType = 2
					content, errCode = PlatformRepo.GetRepoFileContent(Configuration.Gitlab.UserWorkflowsToken, prjId, "README.md")
				}
			}
		}

		// 3. Convert rst/md to html and send the html content back.
		if errCode == 0 {
			var result int
			if readmeType == 1 {
				// Convert rst to html
				result, content = wac.ConvertRSTtoHTML(content)
				if result > 0 {
					c.Data(http.StatusNotFound, "text/html", []byte(content))
					return
				}
			} else {
				// Convert md to html
				md := []byte(content)
				content = string(markdown.ToHTML(md, nil, nil))
			}
		}
		// 5. Return the result
		if len(content) > 0 {
			c.Data(http.StatusOK, "text/html", []byte(content))
		} else {
			c.Data(http.StatusNotFound, "text/html", []byte("readme not found because either passed in project id is not valid or the project does not have readme"))
		}
	} else if workflowType == "prompt" {
		// 1. Check and, if needed, install the workflow or download inputView.ejs, inputContext.json, prompt.mustache?
		db, err := GetDBConnection()
		if err != nil {
			c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_CONNECT_TO_DB, "Failed to connect to internal database.", false, nil, nil, true, &bearerToken, true))
			return
		}
		personalToken, tErr := AccountService.GetUserPlatformPersonalToken(bearerToken)
		if tErr != nil {
			c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_RETRIEVE_PERSONAL_TOKEN, "Failed to retrieve a user's personal token.",
				false, nil, &uWorkflow.Aworkflow, true, &bearerToken, true))
			return
		}
		loggedonUser := SecurityManager.GetLoggedOnUser(bearerToken)
		_, jsErr := wac.CheckWorkflowInstallIfNeeded(db, uWorkflow.Aworkflow, personalToken, bearerToken, loggedonUser.Username, STATUS_TO_BE_EXECUTED)
		if jsErr != nil {
			c.JSON(http.StatusBadRequest, jsErr)
			return
		}
		// 2. Render the input
		htmlView, err := PromptWorkflowHandler.Render(&uWorkflow.Aworkflow, bearerToken, personalToken, loggedonUser)
		if err != nil {
			c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_RENDER_PROMPT_WORKFLOW, "Failed to render a prompt workflow.",
				false, nil, &uWorkflow.Aworkflow, true, &bearerToken, true))
			return
		} else {
			c.Data(http.StatusOK, "text/html", []byte(*htmlView))
		}
	} else {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_WORKFLOW_TYPE_IS_NOT_SUPPORTED, "The workflow type "+workflowType+" is NOT supported.",
			true, &workflow, &uWorkflow.Aworkflow, false, &bearerToken, true))
		return
	}
	wac.RequestProcessedSuccessfully = true
}

func (wac *BaseWorkflowApiController) FindTopic(topics []string, searchTerm string) *string {
	for i, topic := range topics {
		if strings.Contains(topic, searchTerm) {
			return &topics[i]
		}
	}
	return nil
}

func (wac *BaseWorkflowApiController) ConvertRSTtoHTML(content string) (int, string) {
	var result int
	var htmlContent string
	// 1. Dump the .rst content into current dir/tmp.rst
	fo, err := os.Create("tmp.rst")
	if err != nil {
		result = 1
		return result, htmlContent
	}
	defer fo.Close()

	_, err = io.Copy(fo, strings.NewReader(content))
	if err != nil {
		result = 2
		return result, htmlContent
	}

	// 2. Execute rst2html5 tmp.rst tmp.html
	var cmd *exec.Cmd
	cmd = exec.Command("rst2html5", "tmp.rst", "tmp.html")
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		log.Println("Failed to convert rst to html.")
		result = 3
		htmlContent = fmt.Sprintf("Failed to convert rst to html: %v", err)
		return result, htmlContent
	}

	// 3. Load tmp.html
	fileContent, err := os.ReadFile("tmp.html")
	if err != nil {
		log.Println("Failed to load generated html.")
		result = 4
		return result, htmlContent
	}
	htmlContent = string(fileContent)
	// 4. Remove tmp.html
	os.Remove("tmp.rst")
	os.Remove("tmp.html")
	// 5. Return the content of tmp.html
	return result, htmlContent
}

func (wac *BaseWorkflowApiController) GetWorkflowEstimate(c *gin.Context) {
	wac.RequestProcessedSuccessfully = false
	// 0. Check authentication
	if SecurityManager.IsApiAuthenticated(c) > 0 {
		http.Error(c.Writer, "Failed to authenticate.", http.StatusUnauthorized)
		return
	}

	// 1. Parse out the data to an instance of the used workflow model
	bearerToken := c.Request.Header.Get("Authorization")
	wac.BearerToken = bearerToken

	var uWorkflow UsedWorkflow
	if err := c.BindJSON(&uWorkflow); err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_INVALID_USED_WORKFLOW, "Bad UsedWorkflow Object Format.",
			false, nil, nil, false, &bearerToken, false))
		return
	}
	wac.AWorkflow = &uWorkflow.Aworkflow

	// Sanity checks
	if len(uWorkflow.Aworkflow.Parameters) == 0 {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_INVALID_USED_WORKFLOW, "AWorkflow property parameters must be populated.",
			false, nil, nil, false, &bearerToken, false))
		return
	}

	// Make sure ok_prompt is present and populated.
	promptIsPresent := false
	for _, p := range uWorkflow.Aworkflow.Parameters {
		if p.Name == "ok_prompt" && p.ActualValues != nil && len(p.ActualValues[0]) > 0 {
			promptIsPresent = true
			break
		}
	}
	if !promptIsPresent {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_INVALID_USED_WORKFLOW, "AWorkflow property parameter ok_prompt.ActualValues[0] must be populated.",
			false, nil, nil, false, &bearerToken, false))
		return
	}

	// 2. Report to the workflow manager that the workflow was viewed.
	uWorkflow.Status = "WORKFLOW_ESTIMATED"
	var workflow Workflow
	workflow.ExternalId = uWorkflow.Aworkflow.Id
	workflow.Name = uWorkflow.Aworkflow.Name
	workflow.Status = uWorkflow.Status
	workflow.WorkspaceId = uuid.MustParse(Configuration.Workspace.Id)
	workflow.Timestamp = time.Now()

	wac.Workflow = &workflow

	// 3. Check what type a passed aworkflow instance is.
	typeTopic := wac.FindTopic(uWorkflow.Aworkflow.Topics, "type")
	workflowType := "task"
	if typeTopic != nil {
		workflowType = strings.Split(*typeTopic, "=")[1]
	}

	var we WorkflowEstimate
	if workflowType == "task" {
		// 4. Need to call the workspace service endpoint /workflows/estimate when it is ready. For now just return zero cost estimate
		c.JSON(http.StatusOK, we)
	} else if workflowType == "prompt" {
		// 4. Call the AI Service
		we, err := AiServiceApi.GetEstimate(uWorkflow.Aworkflow, bearerToken)
		if err == nil {
			c.JSON(http.StatusOK, we)
		} else {
			c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_ESTIMATE_WORKFLOW, "Failed to get an estimate for the workflow ["+strconv.Itoa(int(uWorkflow.Aworkflow.Id))+"]",
				true, &workflow, &uWorkflow.Aworkflow, false, &bearerToken, true))
			return
		}
	} else {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_WORKFLOW_TYPE_IS_NOT_SUPPORTED, "The workflow type "+workflowType+" is NOT supported.",
			true, &workflow, &uWorkflow.Aworkflow, false, &bearerToken, true))
		return
	}
	wac.RequestProcessedSuccessfully = true
}

// WorkflowsSchedulePost - The method schedules a passed in project
func (wac *BaseWorkflowApiController) WorkflowsSchedulePost(c *gin.Context) {
	// 0. Check authentication
	if SecurityManager.IsApiAuthenticated(c) > 0 {
		http.Error(c.Writer, "Failed to authenticate.", http.StatusUnauthorized)
		return
	}

	bearer_token := c.Request.Header.Get("Authorization")
	// 1. Parse out the data to an instance of the workflow and schedule models
	var aWorkflow AWorkflow
	if err := c.BindJSON(&aWorkflow); err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_INVALID_AWORKFLOW_SCHEDULE, "Bad AWorkflow Object Format.",
			false, nil, nil, false, &bearer_token, true))
		return
	}

	// 2. Check and install the workflow if needed
	db, err := GetDBConnection()
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_CONNECT_TO_DB, "Failed to connect to internal database.", false, nil, nil, true, &bearer_token, true))
		return
	}

	errStr := wac.ScheduleWorkflow(db, aWorkflow, c.Request.Header.Get("Authorization"))
	if len(errStr) > 0 {
		c.JSON(http.StatusOK, gin.H{"Code": STATUS_FAILED_TO_SCHEDULE, "Message": errStr})
	} else {
		c.JSON(http.StatusOK, gin.H{"Code": STATUS_OK, "Message": "Successfully scheduled the task."})
	}
}

func (wac *BaseWorkflowApiController) ScheduleWorkflow(db *gorm.DB, aWorkflow AWorkflow, bearer_token string) string {
	personalToken, tErr := AccountService.GetUserPlatformPersonalToken(bearer_token)
	loggedonUser := SecurityManager.GetLoggedOnUser(bearer_token)
	if tErr != nil {
		return "Failed to retrieve a user's personal token."
	}
	workflow, jsErr := WorkflowApiController.CheckWorkflowInstallIfNeeded(db, aWorkflow, personalToken, bearer_token, loggedonUser.Username, STATUS_TO_BE_SCHEDULED)
	if jsErr != nil {
		return "Failed to install the workflow"
	}

	// 3. Schedule the task
	var schedule WorkflowSchedule
	// Check if the schedule already exists
	result := db.Table("workflow_schedules").Where(clause.Eq{Column: "workflow_id", Value: workflow.Id}).
		Where(clause.Eq{Column: "name", Value: workflow.Name}).First(&schedule)
	if result.RowsAffected > 0 {
		// Update the schedule
		schedule.Start = aWorkflow.Schedule.Start
		schedule.End = aWorkflow.Schedule.End
		schedule.Timestamp = time.Now()
		result = db.Save(&schedule)
		if result.RowsAffected == 0 {
			return "Failed to save the schedule in the db."
		}

		wac.updateParametersIfNeeded(workflow, aWorkflow.Parameters, db)
		return ""
	}

	schedule.Start = aWorkflow.Schedule.Start
	schedule.End = aWorkflow.Schedule.End
	schedule.Name = workflow.Name
	schedule.Id = uuid.New()
	schedule.Timestamp = time.Now()
	schedule.WorkflowId = workflow.Id
	result = db.Create(&schedule)

	if result.RowsAffected == 0 {
		return "Failed to save the schedule in the db."
	}

	// Update parameters
	wac.updateParametersIfNeeded(workflow, aWorkflow.Parameters, db)
	return ""
}

func (wac *BaseWorkflowApiController) updateParametersIfNeeded(workflow *Workflow, passedParameters []AWorkflowParameter, db *gorm.DB) {
	// Update parameters
	var existingParams []WorkflowParameter
	db.Table("workflow_parameters").Where(clause.Eq{Column: "workflow_id", Value: workflow.Id}).Find(&existingParams)
	for _, param := range passedParameters {
		if len(existingParams) > 0 {
			if existingParam := wac.doesParameterByNameExist(existingParams, param.Name); existingParam != nil {
				// Update the parameter
				existingParam.ActualValues = param.ActualValues
				result := db.Save(&existingParam)
				if result.RowsAffected == 0 {
					log.Println("Failed to save the workflow paramater in the db.")
				}
			} else {
				// Add the parameter to the db
				var newDbParam WorkflowParameter
				copier.Copy(&newDbParam, &param)
				newDbParam.Id = uuid.New()
				newDbParam.WorkflowId = workflow.Id
				result := db.Create(&newDbParam)
				if result.RowsAffected == 0 {
					log.Println("Failed to add the workflow paramater in the db.")
				}
			}
		} else {
			// Add the parameter to the db
			var newDbParam WorkflowParameter
			copier.Copy(&newDbParam, &param)
			newDbParam.Id = uuid.New()
			newDbParam.WorkflowId = workflow.Id
			result := db.Create(&newDbParam)
			if result.RowsAffected == 0 {
				log.Println("Failed to add the workflow paramater in the db.")
			}
		}
	}
}

func (wac *BaseWorkflowApiController) doesParameterByNameExist(params []WorkflowParameter, name string) *WorkflowParameter {
	var result *WorkflowParameter
	for _, p := range params {
		if p.Name == name {
			result = &p
			return result
		}
	}
	return result
}

// WorkflowsHistoryGet - The method returns a list of prevoiusly executed tasks.
func (wac *BaseWorkflowApiController) WorkflowsHistoryGet(c *gin.Context) {
	// 0. Check authentication
	if SecurityManager.IsApiAuthenticated(c) > 0 {
		http.Error(c.Writer, "Failed to authenticate.", http.StatusUnauthorized)
		return
	}

	// 1. Pull previously executed tasks
	bearer_token := c.Request.Header.Get("Authorization")
	db, err := GetDBConnection()

	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_CONNECT_TO_DB, "Failed to connect to internal database.", false, nil, nil, true, &bearer_token, true))
		return
	}
	var workflows []Workflow
	result := db.Table("workflows").Find(&workflows).Distinct("name").Limit(5)
	if result.RowsAffected == 0 {
		// 3. If not, install it
		log.Println("No previous workflows found")
		c.Data(http.StatusNotFound, "text/html", []byte("No tasks were executed before."))
	} else {
		bearer_token := c.Request.Header.Get("Authorization")
		personalToken, tErr := AccountService.GetUserPlatformPersonalToken(bearer_token)
		if tErr != nil {
			c.JSON(http.StatusBadRequest, ErrorHandler.ReportError(STATUS_FAILED_TO_RETRIEVE_PERSONAL_TOKEN, "Failed to retrieve a user's personal token.",
				false, nil, nil, true, &bearer_token, true))
			return
		}

		var projects []AWorkflow
		for _, workflow := range workflows {
			project := PlatformRepo.GetPlatformRepo(&workflow, personalToken)
			if project != nil {
				projects = append(projects, *project)
			}
		}
		c.JSON(http.StatusOK, gin.H{"projects": projects})
	}
}