package main

import (
	"log"
	"os"
)

var logger = log.New(os.Stdout, "", log.LstdFlags)

// constants for log levels, starting at 0.
const (
	LogLevelNone = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelDebug
)
