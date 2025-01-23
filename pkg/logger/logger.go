package logger

import (
	"io"
	"log"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

func NewLogger() {
	logFile := lumberjack.Logger{
		Filename:   "router.log",
		MaxSize:    50, // MB
		MaxBackups: 3,
		MaxAge:     28, //days
	}

	// Set log output to the file and console
	mw := io.MultiWriter(os.Stdout, &logFile)
	log.SetOutput(mw)

	// log date-time, filename, and line number
	log.SetFlags(log.Lshortfile | log.LstdFlags)

	log.Println("Logging has been initialized...")
}

func Info(args ...any) {
	log.Println(args...)
}
