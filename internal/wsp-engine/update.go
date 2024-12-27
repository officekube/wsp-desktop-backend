package workspaceEngine

import ()

type UpdateManagerClass struct {
	EnginePath	string
}


func NewUpdateManager() *UpdateManagerClass {
	path, _ := Util.GetEngineInstallationPath()

	updateManager := &UpdateManagerClass{
		EnginePath: path,
	}
	return updateManager
}


func (um *UpdateManagerClass) CheckAndUpdate() {
	// Implement once the GitHub CD/release pipelines are in place
}
