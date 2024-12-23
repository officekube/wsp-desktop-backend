package main

import (
	"log"
	oss "workspace-engine/internal/wsp-engine"
	//platform "workspace-engine/internal/wsp-engine/platform"
)

func main() {
	// 1. Create a gin engine
	engine := oss.NewEngine()
	// 2. Instantiate core objects by type - depends on which version we need to run - oss or platform.
	//err := platform.Util.InitCore(engine)
	err := oss.Util.InitCore(engine)
	if err != nil {
		panic("Failed to load the core engine components.")
	}

	log.Fatal(engine.Run(":8888"))
}
