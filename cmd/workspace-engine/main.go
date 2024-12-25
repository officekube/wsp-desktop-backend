package main

import (
	"log"
	oss "workspace-engine/internal/wsp-engine"
)

func main() {
	// 1. Create a gin engine
	engine := oss.NewEngine()
	// 2. Instantiate core objects by type
	err := oss.Util.InitCore(engine)
	if err != nil {
		panic("Failed to load the core engine components.")
	}

	log.Fatal(engine.Run(":8888"))
}
