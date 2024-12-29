package workspaceEngine

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Interface
type IConfigManager interface {
	Init() error
	LoadWorkspaceConfig(ignoreCache bool) error
	UpdateWorkspaceConfig(key string, value interface{}) error
	LoadWorkspaceConfigFromWorkspaceService(userToken string) error
}

type BaseConfigMgrClass struct {
	IConfigManager
}

//var ConfigMgr = &ConfigMgrClass{}
var ConfigMgr IConfigManager

var Configuration *ConfigurationInfo

func InitBaseConfigManager() error {
	ConfigMgr = &BaseConfigMgrClass{}
	// Initialize gin engine
	return ConfigMgr.Init()
}

type ConfigurationInfo struct {
	Frontend 						FrontendInfo
	Engine				 			EngineInfo
	Guard	 						GuardInfo
	Database                    	DatabaseInfo
	Workspace                   	WorkspaceInfo
	Workflow                    	WorkflowInfo
	App                          	AppInfo
	Security                     	SecurityInfo
	WorkflowManager              	WorkflowManagerInfo
	OfficeKubeIdP                  	OfficeKubeIdPInfo
	WorkspaceService             	WorkspaceServiceInfo
	AccountService               	AccountServiceInfo
	ResourceManager              	ResourceManagerInfo
	PlatformDomain               	string
	Apps                         	[]App
	TcpDumps                     	[]TcpDumpFlags
	WorkflowEnvironmentVariables	[]WorkflowEnvironmentVarInfo
	AppEnvironmentVariables      	[]AppEnvironmentVarInfo
	PlatformPackageRepo          	PlatformPackageRepoInfo
	OKWorkflowPlatform             	OKWorkflowPlatformInfo
	TemplateSettings             	[]TemplateSettingInfo
	AiService                    	AiServiceInfo
	LoggingAgent				 	LoggingAgentInfo
}

type TcpDumpFlags struct {
	Name  string
	Flags []string
}

type DatabaseInfo struct {
	Path string
}

type WorkspaceInfo struct {
	Id     string
	Domain string
	// 0 - not launched, 1 - scheduled at the start of the engine, 2 - executed.
	FirstTimeLaunched int
	Type	string
}

type WorkflowInfo struct {
	InstallationFolder string
	ProductionBranch   string
	GitBaseUrl         string
}

type AppInfo struct {
	InstallationFolder string
	ProductionBranch   string
}

type WorkflowManagerInfo struct {
	Endpoint string
}

type WorkspaceServiceInfo struct {
	Endpoint string
}

type AccountServiceInfo struct {
	Endpoint string
}

type AiServiceInfo struct {
	Endpoint string
}

type ResourceManagerInfo struct {
	Endpoint string
}

type SecurityInfo struct {
	OAuth2 OAuth2
}

type OAuth2 struct {
	ClientID       string
	ApiRedirectURL string
	WebRedirectURL string
	IssuerUrl      string
	Scopes         []string
	UrlBase        string
}

type FrontendInfo struct {
	FirstTimeDialogYTUrl        string
	FirstTimeLaunched           bool
	CreateTaskScaffoldingTaskId int
	AIPromptTaskId				int
	Feedback                    FeedbackInfo
	Version						string
}

type FeedbackInfo struct {
	WorkflowId int
	WebUrl     string
}

type OfficeKubeIdPInfo struct {
	Realm string
	UrlBase         string
}

type WorkflowEnvironmentVarInfo struct {
	Name  string
	Value string
}

type AppEnvironmentVarInfo struct {
	Name  string
	Value string
}

type PlatformPackageRepoInfo struct {
	TokenName  string
	TokenValue string
	Protocol   string
	UrlBase    string
}

type OKWorkflowPlatformInfo struct {
	BaseUrl                string
	WorkflowRepoBranch     string
	PlatformWorkflowsToken string
	UserWorkflowsToken     string

	AppsToken string // might be used in ViewApp if we decide to move /apps/view endpoint from the workflow manager to here.
}

type TemplateSettingInfo struct {
	Name  string
	Value string
}

type VersionInfo struct {
	engineVersion 	string
	wspUIVersion	string
}

type EngineInfo struct {
	Version 	string
}

type GuardInfo struct {
	Version 	string
}

type LoggingAgentInfo struct {
	AwsAccessKey		string
	AwsRegion			string
	AwsSecretAccessKey	string
	InstallCommand		string
	VectorConfig		string
}

func (cgm *BaseConfigMgrClass) Init() error {
	err := ConfigMgr.LoadWorkspaceConfig(false)
	if err != nil {
		return err
	}
	return nil
}

func (cgm *BaseConfigMgrClass) LoadWorkspaceConfig(ignoreCache bool) error {
	var err error
	if Configuration != nil && !ignoreCache {
		return nil
	}
	// 1. Load config
	path, err := cgm.getConfigurationFilePath()
	if err != nil {
		return err
	}
	viper.SetConfigName("workspace")
	// Set the path to look for the configurations file
	viper.AddConfigPath(path)
	viper.SetConfigType("yml")

	if err = viper.ReadInConfig(); err != nil {
		log.Println("Failed to read the workspace configuration.")
		return err
	}

	if err := viper.Unmarshal(&Configuration); err != nil {
		log.Println("Failed to parse the workspace configuration.")
		return err
	}
	return nil
}

func (cgm *BaseConfigMgrClass) UpdateWorkspaceConfig(key string, value interface{}) error {
	var err error
	viper.SetConfigName("workspace")
	path, err := cgm.getConfigurationFilePath()
	if err != nil {
		return err
	}
	viper.AddConfigPath(path)
	viper.SetConfigType("yml")
	viper.Set(key, value)
	err = cgm.LoadWorkspaceConfig(true)
	if err != nil { return err }
	err2 := viper.WriteConfig()
	return err2
}

func (cgm *BaseConfigMgrClass) LoadWorkspaceConfigFromWorkspaceService(userToken string) error {
	var err error

	// 1. Load config json from the workspace service
	ymlConfig, err := WorkspaceService.GetWspEngineSettings(userToken, Configuration.Workspace.Id)
	if err != nil {
		log.Println("Failed to fetch configuration from the workspace service: ", err)
		return err
	}

	viper.SetConfigType("yaml")

	if err = viper.ReadConfig(bytes.NewBuffer(ymlConfig)); err != nil {
		log.Println("Failed to read the workspace configuration loaded from the workspace service.")
		return err
	}

	// Preserve firsttimelaunched/FirstTimeDialogYTUrl/Version settings
	var oldFTLValue = Configuration.Frontend.FirstTimeLaunched
	var oldVValue = Configuration.Frontend.Version
	var oldFTDUValue = Configuration.Frontend.FirstTimeDialogYTUrl	

	if err = viper.Unmarshal(&Configuration); err != nil {
		log.Println("Failed to parse the workspace configuration loaded from the workspace service.")
		return err
	}

	// Override firsttimedialogyturl from the template settings
	for _, ts := range Configuration.TemplateSettings {
		if ts.Name == "workspaceTutorualUrl" && len(ts.Value) > 0 {
			Configuration.Frontend.FirstTimeDialogYTUrl = ts.Value
			oldFTDUValue = Configuration.Frontend.FirstTimeDialogYTUrl	
			break
		}
	}

	// Restore Frontend settings.
	Configuration.Frontend.FirstTimeLaunched = oldFTLValue
	Configuration.Frontend.FirstTimeDialogYTUrl = oldFTDUValue
	Configuration.Frontend.Version = oldVValue

	// Ensure that all folders referenced by relevant config settings exist
	if _, err := os.Stat(Configuration.Database.Path); errors.Is(err, os.ErrNotExist) {
		os.Mkdir(Configuration.Database.Path, os.ModePerm)
	}
	if _, err := os.Stat(Configuration.Workflow.InstallationFolder); errors.Is(err, os.ErrNotExist) {
		os.Mkdir(Configuration.Workflow.InstallationFolder, os.ModePerm)
	}
	if _, err := os.Stat(Configuration.App.InstallationFolder); errors.Is(err, os.ErrNotExist) {
		os.Mkdir(Configuration.App.InstallationFolder, os.ModePerm)
	}
	if _, err := os.Stat(Configuration.App.InstallationFolder); errors.Is(err, os.ErrNotExist) {
		os.Mkdir(Configuration.App.InstallationFolder, os.ModePerm)
	}
	return nil
}

func (cgm *BaseConfigMgrClass) getConfigurationFilePath() (path string, err error) {
	volPath := os.Getenv("volume_path")
	if len(volPath) > 0 {
		// If volume_path is populated, its value is used.
		path = volPath
	} else {
		// If volume_path is not populated, the engine assumes the file to be in the current folder.
		path, err = Util.GetEngineInstallationPath()
		if err != nil {
			return path, err
		}
	}
	// Validate that the config file exists
	configFilePath := filepath.Join(path, "workspace.yml")
	if _, err := os.Stat(configFilePath); errors.Is(err, os.ErrNotExist) {
		// config file does not exist
		return path, err
	}
	return path, nil
}