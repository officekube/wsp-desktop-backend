package workspaceEngine

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

type PromptWorkflowHandlerClass struct {
}

var PromptWorkflowHandler = &PromptWorkflowHandlerClass{}

func (pwh *PromptWorkflowHandlerClass) Render(aw *AWorkflow, bearer_token string, personalToken string, user *User) (html *string, err error) {
	taskPath := filepath.Join(Configuration.Workflow.InstallationFolder, aw.Path)
	// 1. Make sure inputView.ejs exists
	if _, err = os.Stat(filepath.Join(taskPath, "inputView.ejs")); errors.Is(err, os.ErrNotExist) {
		return
	}
	// 2. Make sure prompt.mustache exists
	if _, err = os.Stat(filepath.Join(taskPath, "prompt.mustache")); errors.Is(err, os.ErrNotExist) {
		return
	}
	var promptCtx map[string]interface{}
	// 3. Load inputContext.json (if exists, the file includes data that the template will use for its parameters).
	ctxJson, lErr := os.ReadFile(filepath.Join(taskPath, "inputContext.json"))
	if lErr == nil {
		lErr = json.Unmarshal(ctxJson, &promptCtx)
		if lErr != nil {
			log.Println("Failed to load prompt context. Keep processing...")
		}
	}
	// 3. Load a platform context. This should include various data such as workspace id, workspace type, etc that the template might use. 
	// See how the engine populates relevant variables before in addEnvironmentVariables
	promptCtx["ok_platform_domain"] = Configuration.PlatformDomain
	promptCtx["workspace_id"] = Configuration.Workspace.Id
	promptCtx["ok_access_token"] = personalToken
	promptCtx["ok_bearer_token"] = bearer_token
	promptCtx["username"] = user.Username
	for _, e := range Configuration.WorkflowEnvironmentVariables {
		promptCtx[e.Name] = e.Value
	}
	// Add workflow parameters
	promptCtx["hiddenParameters"] = aw.Parameters
	// Add prompt
	promptBytes, _ := os.ReadFile(filepath.Join(taskPath, "prompt.mustache"))
	promptCtx["ok_prompt"] = string(promptBytes)

    // 4. Copy base.ejs - this is a template bundled with the engine and should add a hidden field with id ok_prompt as well JS code that would on every change of each template parameter (on_change handler?) update the hidden field. The value for the field will be loaded from prompt.mustache. The base.ejs might rather embed the inputView.ejs surrounding its content with an html form - that way changes of every parameter defined in inputView.ejs can be tracked and carried over to the prompt. Additionally, the base.ejs/engine should add HTML code that will add a hidden field for each parameter defined in workflow.yaml/aworkflow instance that has in its attribute tags a value prompt and attribute displayed set to false. Accordingly, the base.ejs/engine should add JS code that would update the field ok_prompt everytime a value of every prompt parameter is changed by the user.
	path, _ := Util.GetEngineInstallationPath()
	_, err = Util.CopyFile(filepath.Join(path, "ejsTemplates", "base.ejs"), filepath.Join(taskPath, "base.ejs"))
	if err != nil {
		return
	}
	// 5. Dumpt context into context.json
	jsonBody, err := json.Marshal(promptCtx)
	if err != nil {
		return
	}
	ctxFileName := filepath.Join(taskPath, "context.json")
	ctxFile, _ := os.Create(ctxFileName)
	ctxFile.Write(jsonBody)
	ctxFile.Close()
	// 5. Render the html content using EJS CLI and return the content.
	// FIXME: write a code in base.ejs to create a number of hidden fields and js code based on prompt parameters 
	// that the workflow author should define in workflow.yml and we should loop through here and build yet another context?
	// to be passed into base.ejs?
	cmd := exec.Command("ejs", "base.ejs", "-f", ctxFileName, "-o", filepath.Join(taskPath, "inputView.html"))
	cmd.Dir = taskPath	
	cmd.Env = os.Environ()
	err = cmd.Run()
	os.Remove(ctxFileName)
	if err != nil {
		return
	}
	bytes, err := os.ReadFile(filepath.Join(taskPath, "inputView.html"))
	if err != nil {
		return
	}
	tmpStr := string(bytes)
	return &tmpStr, nil
}