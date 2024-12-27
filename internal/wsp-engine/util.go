package workspaceEngine

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

type UtilClass struct {
}

var Util = &UtilClass{}

func (u *UtilClass) CheckDependenciesInstallIfNeeded(path string) (err error) {
	// 1. Load dependencies section from workflow.yml
	viper.Reset()
	viper.SetConfigName("workflow")
	// Set the path to look for the configurations file
	viper.AddConfigPath(path)
	viper.SetConfigType("yml")

	if err = viper.ReadInConfig(); err != nil {
		log.Println("Failed to read the workflow.yml.")
		return err
	}

	var wflYml WorkflowYml
	if err = viper.Unmarshal(&wflYml); err != nil {
		log.Println("Failed to parse the workflow.yml.")
		return err
	}
	// 2. Loop through dependencies and their packages
	for _, d := range wflYml.Dependencies {
		for _, p := range d.Packages {
			// 3. If possible, check if the package is installed. If not, install it
			u.checkInstallPackage(p.Name, p.Type, p.Version, p.Source)
		}
		// 4. Install the dependency itself.
		u.checkInstallPackage(d.Name, d.Type, d.Version, d.Source)
	}

	return nil
}

func (u *UtilClass) checkInstallPackage(name string, pType string, version string, source string) (err error) {
	var checkCmd *exec.Cmd
	var installCmd *exec.Cmd
	var packageName = name
	var statusCode = ""
	var errMsg = ""
	var stdChkBuffer bytes.Buffer
	var checkCmdOutputStr string

	shellCmd := "bash"
	shellCmdArg := "-c"
	grepCmd := "grep"
	if runtime.GOOS == "windows" {
		shellCmd = "cmd.exe"
		shellCmdArg = "/C"
		grepCmd = "findstr.exe"
	}
	// 1. Prepare a command to check if the package is already installed
	switch pType {
	case "npm":
		if len(version) > 0 {
			packageName += "@" + version
		}
		checkCmd = exec.Command(shellCmd, shellCmdArg, "npm list -g | " + grepCmd + " " + packageName)
	case "apt":
		// Based on use of dpkg-query https://man7.org/linux/man-pages/man1/dpkg-query.1.html
		var dpkgCmdStr = "dpkg-query -W --showformat='${Package}=>${Status}' " + packageName + "| " + grepCmd + " \"install ok installed\""
		if len(version) > 0 {
			dpkgCmdStr = "dpkg-query -W --showformat='${Package}=${Version}=>${Status}' " + packageName + "| " + grepCmd + " \"" + version + "=>install ok installed\""
		}
		checkCmd = exec.Command(shellCmd, shellCmdArg, dpkgCmdStr)
	case "pip":
	case "pip3":
		// The package name could be in the format package[option] which we do not need for the check
		if strings.Contains(packageName, "[]") && strings.Contains(packageName, "]") {
			packageName = strings.Split(packageName, "[")[0]
		}
		// Sudo is used to surpress a possible warning 'WARNING: The directory '/work/.cache/pip' or
		// its parent directory is not owned or is not writable by the current user.
		// The cache has been disabled. Check the permissions and owner of that directory.
		// If executing pip with sudo, you may want sudo's -H flag.'
		var pipCmdStr = "pip3 freeze | " + grepCmd + " " + packageName
		if runtime.GOOS != "windows" {
			pipCmdStr = "sudo " + pipCmdStr
		}
		checkCmd = exec.Command(shellCmd, shellCmdArg, pipCmdStr)
	}

	// 2. Check if the package is installed
	if checkCmd != nil {
		checkCmd.Env = os.Environ()
		// The approach below does not capture the stderr
		//checkCmdOutput, err := checkCmd.Output()
		//checkCmdOutputStr = string(checkCmdOutput)
		mw := io.MultiWriter(os.Stdout, &stdChkBuffer)
		checkCmd.Stdout = mw
		checkCmd.Stderr = mw
		err = checkCmd.Run()

		checkCmdOutputStr = stdChkBuffer.String()

		log.Println(checkCmdOutputStr)

		if err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				// The program has exited with an exit code != 0

				// This works on both Unix and Windows. Although package syscall is generally platform dependent, WaitStatus is
				// defined for both Unix and Windows and in both cases has an ExitStatus() method with the same signature.
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
					reportError := true
					switch pType {
					case "npm":
					case "pip":
					case "pip3":
						//if status == 256 {
						if status.ExitStatus() == 1 {
							// In case of npm/pip3 it simply means that the package is not installed
							reportError = false
						}
					}

					if reportError {
						statusCode = STATUS_FAILED_TO_CHECK_A_DEPENDENCY_PACKAGE
						errMsg = "Checking existence of a package " + name + " failed with the status code: " + fmt.Sprint(status.ExitStatus())
						log.Println(statusCode)
						log.Println(errMsg)
					}
				}
			} else {
				statusCode = STATUS_FAILED_TO_CHECK_A_DEPENDENCY_PACKAGE
				errMsg = "Package name: " + name
				log.Println(statusCode)
				log.Println(errMsg)
			}
		}
	}

	// 3. Prepare a command to install the package
	var stdInstBuffer bytes.Buffer
	switch pType {
	case "npm":
		if len(checkCmdOutputStr) == 0 {
			// Package is not installed
			installCmd = exec.Command(shellCmd, shellCmdArg, "npm install -g " + packageName)
			log.Println("The package " + packageName + " is not installed. Installing it...")
		} else {
			log.Println("The package " + packageName + " is already installed.")
		}
	case "apt":
		if len(version) > 0 {
			packageName += "=" + version
		}

		if len(checkCmdOutputStr) == 0 {
			// Package is not installed
			installCmd = exec.Command(shellCmd, shellCmdArg, "apt install -qy " + packageName)
			log.Println("The package " + packageName + " is not installed. Installing it...")
		} else {
			// There could be still the case when the package is not installed even if the checkCmdOutputStr is not empty.
			var expectedStr = packageName + "=>install ok installed\n"
			if expectedStr != checkCmdOutputStr {
				// Package is not installed
				installCmd = exec.Command(shellCmd, shellCmdArg, "apt install -qy " + packageName)
				log.Println("The package " + packageName + " is not installed. Installing it...")
			} else {
				log.Println("The package " + packageName + " is already installed. ")
			}
		}
	case "pip":
	case "pip3":
		if len(version) > 0 {
			packageName += "==" + version
		}

		var externalRepo = ""
		// If the source is provided a package is expected to be installed from an external repo
		if len(source) > 0 {
			if strings.Contains(source, "ok/") == true {
				packageId := strings.Split(source, "/")[1]
				externalRepo = " --extra-index-url " + Configuration.PlatformPackageRepo.Protocol + "://" +
					Configuration.PlatformPackageRepo.TokenName + ":" + Configuration.PlatformPackageRepo.TokenValue +
					"@" + Configuration.PlatformPackageRepo.UrlBase + "/projects/" + packageId + "/packages/pypi/simple"
			} else {
				externalRepo = " --extra-index-url " + source
			}
		}

		if len(checkCmdOutputStr) == 0 {
			// Package is not installed
			// https://stackoverflow.com/questions/8400382/python-pip-silent-install
			installCmd = exec.Command(shellCmd, shellCmdArg, "pip3 install " + packageName + " -q -q -q --exists-action i" + externalRepo)
			log.Println("The package " + packageName + " is not installed. Installing it...")
		} else {
			// There could be still the case when the package is not installed even if the checkCmdOutputStr is not empty.
			match := false
			if len(version) > 0 {
				match = strings.Contains(checkCmdOutputStr, packageName)
			} else {
				match = strings.Contains(checkCmdOutputStr, name)
			}
			if match {
				log.Println("The package " + packageName + " is already installed. ")
			} else {
				// Package is not installed
				// https://stackoverflow.com/questions/8400382/python-pip-silent-install
				installCmd = exec.Command(shellCmd, shellCmdArg, "pip3 install " + packageName + " -q -q -q --exists-action i" + externalRepo)
				log.Println("The package " + packageName + " is not installed. Installing it...")
			}
		}
	}

	// 4. Install the package
	if installCmd != nil {
		installCmd.Env = os.Environ()
		mw := io.MultiWriter(os.Stdout, &stdInstBuffer)
		installCmd.Stdout = mw
		installCmd.Stderr = mw
		err = installCmd.Run()

		log.Println(stdInstBuffer.String())

		if err != nil {
			if exiterr, ok := err.(*exec.ExitError); ok {
				if status, ok := exiterr.Sys().(syscall.WaitStatus); ok {
					statusCode = STATUS_FAILED_TO_CHECK_A_DEPENDENCY_PACKAGE
					errMsg = "Installation of a package " + name + " failed with the status code: " + fmt.Sprint(status.ExitStatus())
					log.Println(statusCode)
					log.Println(errMsg)
				}
			} else {
				statusCode = STATUS_FAILED_TO_CHECK_A_DEPENDENCY_PACKAGE
				errMsg = "Package name: " + name
				log.Println(statusCode)
				log.Println(errMsg)
			}
		} else {
			log.Println("Package " + name + " has been installed.")
		}
	}
	return nil
}

func (u *UtilClass) CopyFile(src, dst string) (int64, error) {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return 0, err
	}

	if !sourceFileStat.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
}

func (u *UtilClass) InitCore(ginEngine *gin.Engine) (*error) {
	// 0. Initialize logger
	err := InitBaseLoggingAgent()
	if err != nil {
		return err
	}
	// 1. Initialize Configuration Manager
	if err2 := InitBaseConfigManager(); err2!= nil { return &err2 }
	// 2. Initialize Security Manager
	if err = InitBaseSecurityManager(ginEngine); err != nil { return err}
	// 3. Initialize Workspace Service
	if err = InitBaseWorkspaceService(); err != nil { return err}
	// 4. Initialize the Startup Workflow worker
	if err = InitBaseStartupWorkflowWorker(); err != nil { return err}
	// 5. Initialize Error Handler
	if err = InitBaseErrorHandler(); err != nil { return err}
	// 6. Initialize Workflow API Controller
	if err = InitBaseWorkflowApiController(ginEngine); err != nil { return err}
	// 7. Initialize App API Controller
	if err = InitBaseAppApiController(ginEngine); err != nil { return err}
	// 8. Initialize Engine API Controoler
	if err = InitBaseEngineApiController(ginEngine); err != nil { return err}

	return nil
}

func (u *UtilClass) GetVolumePath() (path string, err error) {
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
	// Validate that the path exists
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		// path does not exist
		return path, err
	}
	return path, nil
}

func (u *UtilClass) GetEngineInstallationPath() (path string, err error) {
	// The actual location of the engine may be different from the current directory. So, the logic to determine that should
	// be placed in this method rather than relying on os.Getwd()
	path, err = os.Getwd()
	return path, err
}