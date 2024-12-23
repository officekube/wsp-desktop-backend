package workspaceEngine

import (
	"io"
	"log"
	"os"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Interface
type ILoggingAgent interface {
	Init()	(err *error)
}

type BaseLoggingAgent struct {
	IStartupWorkflowWorker
}

var LoggingAgent IStartupWorkflowWorker

func InitBaseLoggingAgent() *error {

	LoggingAgent = &BaseLoggingAgent{}
	return LoggingAgent.Init()
}

func (la *BaseLoggingAgent) Init() (*error) {
	// 1. Set log output to a file in current directory
    // logFile, err := os.OpenFile("enginge.log", os.O_APPEND|os.O_RDWR|os.O_CREATE, 0644)
    // if err != nil {
    //     return &err
    // }
	// Set log rotation
	logFile := lumberjack.Logger{
		Filename:   "enginge.log", 
		MaxSize:    50, // MB
		MaxBackups: 3,   
		MaxAge:     28,   //days
	}
    //defer logFile.Close()

    // Set log output to the file and console
	mw := io.MultiWriter(os.Stdout, &logFile)
    log.SetOutput(mw)

    // log date-time, filename, and line number
    log.SetFlags(log.Lshortfile | log.LstdFlags)

    log.Println("Logging has been initialized...")
	return nil
}