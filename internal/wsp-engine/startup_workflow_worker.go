package workspaceEngine

import (
	"context"
	"log"
	"sync"
	"time"
)

// Interface
type IStartupWorkflowWorker interface {
	Init()	(err *error)
}


const (
    DEFAULT_IDLE_INTERVAL_IN_SECONDS = 10
    NEWVERSIONCHECK_INTERVAL_IN_MINUTES   = 15
)

type BaseStartupWorkflowWorker struct {
	IStartupWorkflowWorker
	IdleInterval 	time.Duration
	Context			context.Context
}

var StartupWorkflowWorker IStartupWorkflowWorker

func InitBaseStartupWorkflowWorker() *error {
	idleInterval := time.Duration(DEFAULT_IDLE_INTERVAL_IN_SECONDS) * time.Millisecond

	StartupWorkflowWorker = &BaseStartupWorkflowWorker {
		IdleInterval: idleInterval,
		Context: context.Background(),
	}
	return StartupWorkflowWorker.Init()
}

func (sww *BaseStartupWorkflowWorker) Init() (err *error) {
	log.Println("Starting the StartupWorkflowWorker")
	// WaitGroup -  adds workers untill no more messages are in queue
	wg := &sync.WaitGroup{}
	wg.Add(1)

    go sww.StartNewVersionCheck("base")

	return nil
}

func (sww *BaseStartupWorkflowWorker) StartNewVersionCheck(wspType string) {
    ticker := time.NewTicker(NEWVERSIONCHECK_INTERVAL_IN_MINUTES * time.Minute)
    defer ticker.Stop()

    for {
        select {
        case <-sww.Context.Done():
            return
        case <-ticker.C:
			um := NewUpdateManager()
            um.CheckAndUpdate()
        }
    }
}
