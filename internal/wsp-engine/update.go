package workspaceEngine

import (
	"archive/zip"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// const wspEngineBasePath = "/usr/local/wsp-engine"
//const wspEngineBasePath = "/usr/local/wsp-engine-test"

type UpdateManagerClass struct {
	enginePath	string
}


func NewUpdateManager() *UpdateManagerClass {
	path, _ := Util.GetEngineInstallationPath()

	updateManager := &UpdateManagerClass{
		enginePath: path,
	}
	return updateManager
}


func (um *UpdateManagerClass) CheckAndUpdate(workspaceId string, updateCheckRequest *UpdateCheckRequest) {

	updateResponse, err := WorkspaceService.GetUpdateCheck(workspaceId, updateCheckRequest)
	if err != nil {
		log.Println("Failed to check update:", err)
		return
	}

	if updateResponse.UIUpdateAvailable {
		log.Println("UI update available. Downloading...")
		err := um.downloadAndApplyUpdate(updateResponse.UIDownloadUrl, "ui", updateResponse.UIVersion)
		if err != nil {
			log.Println("Failed to download and apply UI update:", err)
			return
		}
		um.applyUIUpdate()
	}
	
	if updateResponse.EngineUpdateAvailable {
		log.Println("Engine update available. Downloading...")
		err := um.downloadAndApplyUpdate(updateResponse.EngineDownloadUrl, "engine", updateResponse.EngineVersion)
		if err != nil {
			log.Println("Failed to download and apply engine update:", err)
			return
		}
	}

	if updateResponse.GuardUpdateAvailable {
		log.Println("Guard update available. Downloading...")
		err := um.downloadAndApplyUpdate(updateResponse.GuardDownloadUrl, "guard", updateResponse.GuardVersion)
		if err != nil {
			log.Println("Failed to download and apply guard update:", err)
			return
		}
	}
}

func (um *UpdateManagerClass) downloadAndApplyUpdate(url string, updateType string, version string) error {
    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return errors.New("failed to download update from " + url + ": " + resp.Status)
    }

    contentLength := resp.Header.Get("Content-Length")
    expectedSize, err := strconv.Atoi(contentLength)
    if err != nil {
        return fmt.Errorf("failed to parse content length: %w", err)
    }

    tempFile, err := ioutil.TempFile("", "update-*.zip")
    if err != nil {
        return err
    }
    defer tempFile.Close()

    hasher := crc32.NewIEEE()
    multiWriter := io.MultiWriter(tempFile, hasher)

    written, err := io.Copy(multiWriter, resp.Body)
    if err != nil {
        return err
    }

    if int(written) != expectedSize {
        return errors.New("downloaded file size does not match expected size")
    }

    updatePath := filepath.Join(um.enginePath, "update", updateType)
    if err := os.MkdirAll(updatePath, os.ModePerm); err != nil {
        return err
    }

    if err := um.unzip(tempFile.Name(), updatePath); err != nil {
        return err
    }

    if updateType == "engine" {
		ConfigMgr.UpdateWorkspaceConfig("engine.version", version)
        if err := um.updateAndStopEngine(); err != nil {
            return fmt.Errorf("failed to trigger supervisor script: %w", err)
        }
    } else if updateType == "guard" {
		ConfigMgr.UpdateWorkspaceConfig("guard.version", version)
        if err := um.updateAndStopGuard(); err != nil {
            return fmt.Errorf("failed to trigger supervisor script: %w", err)
        }
    } else if updateType == "ui" {
		ConfigMgr.UpdateWorkspaceConfig("frontend.version", version)
        log.Println("UI update downloaded and applied.")
    }

    return nil
}

func (um *UpdateManagerClass) updateAndStopEngine() error {
	// FIXME: the process below will not work on OSS and desktop versions of the workspace.
	// The approach implemented below relies on supervisord automatically restarting the engine once it is stopped.
	// So, the script updateAndStopEngine.sh replaces the engine binary with its new version and simply stops the engine.
	// The downside is that during a short downtime no requests will be handled. To address this issue a better approach should be implemented.
	// It is described here https://blog.cloudflare.com/graceful-upgrades-in-go/ and the source is here https://github.com/cloudflare/tableflip?tab=readme-ov-file
	// But it involves a much greater effort, so not doing it for now...
	supPath := filepath.Join(um.enginePath, "updateAndStopEngine.sh")
    cmd := exec.Command("/bin/bash", supPath)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}

func (um *UpdateManagerClass) updateAndStopGuard() error {
	// FIXME: This method and updateAndStopGuard.sh should really be killed and 
	// updateAndStopEngine()/updateAndStopEngine.sh should be refactored to be used for both engine and guard.
	supPath := filepath.Join(um.enginePath, "updateAndStopGuard.sh")
    cmd := exec.Command("/bin/bash", supPath)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}


func (um *UpdateManagerClass) copyAndReplace(src, dest string) error {
	destInfo, err := os.Stat(dest)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if destInfo != nil && destInfo.IsDir() {
		// Destination is a directory, handle normally
		entries, err := ioutil.ReadDir(src)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			// Skip hidden files
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}

			srcPath := filepath.Join(src, entry.Name())
			destPath := filepath.Join(dest, entry.Name())

			if entry.IsDir() {
				if err := os.MkdirAll(destPath, os.ModePerm); err != nil {
					return err
				}
				if err := um.copyAndReplace(srcPath, destPath); err != nil {
					return err
				}
			} else {
				input, err := ioutil.ReadFile(srcPath)
				if err != nil {
					return err
				}
				err = ioutil.WriteFile(destPath, input, entry.Mode())
				if err != nil {
					return err
				}
			}
		}
	} else {
		// Destination is a file, handle as single file copy
		entries, err := ioutil.ReadDir(src)
		if err != nil {
			return err
		}

		relevantEntries := []os.FileInfo{}
		for _, entry := range entries {
			// Skip hidden files
			if !entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				relevantEntries = append(relevantEntries, entry)
			}
		}

		if len(relevantEntries) == 1 {
			srcPath := filepath.Join(src, relevantEntries[0].Name())
			input, err := ioutil.ReadFile(srcPath)
			if err != nil {
				return err
			}
			err = ioutil.WriteFile(dest, input, relevantEntries[0].Mode())
			if err != nil {
				return err
			}
		} else {
			return errors.New("expected a single file in the update source directory")
		}
	}

	return nil
}

func (um *UpdateManagerClass) unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return errors.New("invalid file path")
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (um *UpdateManagerClass) applyUIUpdate() {
	log.Println("Applying UI update...")
	// Copy all contents from update/ui to /usr/local/wsp-engine/wui
	srcPath := filepath.Join(um.enginePath, "update", "ui/build")
	destPath := filepath.Join(um.enginePath, "wui")
	if err := um.copyAndReplace(srcPath, destPath); err != nil {
		log.Println("Failed to apply UI update:", err)
	}
}