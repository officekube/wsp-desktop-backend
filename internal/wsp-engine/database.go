package workspaceEngine

import (
	"path/filepath"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var dbConnection *gorm.DB

const STRING_ARRAY_FIELD_VALUE_SEPARATOR = ";;;"

func GetDBConnection() (db *gorm.DB, err error) {
	// 1. Establish connection
	if(dbConnection == nil) {
		if db, err = gorm.Open(sqlite.Open(filepath.Join(Configuration.Database.Path, "workspace-engine.db")), &gorm.Config{}); err != nil {
			return db, err
		} else {
			// 2. Make sure all relevant tables exist
			db.AutoMigrate(&Workflow{}, &WorkflowSchedule{}, &WorkflowParameter{}, &App{}, &AppParameter{})
			dbConnection = db
		}
	}
	return dbConnection, nil
}