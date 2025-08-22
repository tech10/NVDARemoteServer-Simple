package main

import (
	"log"
	"os"
	"strconv"
	"sync"
)

// Constants for log levels, starting at 0.
// Each log level is more verbose than the prior log level.
const (
	LogLevelNone = iota
	LogLevelInfo
	LogLevelWarn
	LogLevelError
	LogLevelDebug
	LogLevelIntercept
	LogLevelMax // maximum log level should always be LogLevelMax-1
)

// LogLevelStr is an int with a Stringer interface.
type LogLevelStr int

// String implements Stringer for LogLevelStr.
func (l LogLevelStr) String() string {
	switch l {
	case LogLevelNone:
		return "none"
	case LogLevelInfo:
		return "info"
	case LogLevelWarn:
		return "warn"
	case LogLevelError:
		return "error"
	case LogLevelDebug:
		return "debug"
	case LogLevelIntercept:
		return "intercept"
	default:
		return strconv.Itoa(int(l))
	}
}

var logger *Logger

// Logger defines a logger that is used with the various log levels.
type Logger struct {
	level  LogLevelStr
	logger *log.Logger
	mu     sync.Mutex
}

// NewLogger creates a logger with the level set,
// providing various verbocity levels for logging.
//
// If level is less than the minimum log level,
// it wil be set to the minimum log level.
// If level is greater than the maximum log level,
// it will be set to the maximum log level.
func NewLogger(level int) *Logger {
	msgpost := "Logger created."
	if level < LogLevelNone {
		msgpost += " Initial value less than valid range at " + strconv.Itoa(level) + "."
		level = LogLevelNone
	} else if level > LogLevelMax-1 {
		msgpost += " Initial value greater than valid range at " + strconv.Itoa(level) + "."
		level = LogLevelMax - 1
	}
	ll := LogLevelStr(level)
	msgpost += " Using level: " + ll.String() + "."

	l := &Logger{
		level:  ll,
		logger: log.New(os.Stdout, "", log.LstdFlags),
	}

	l.Debugf("%s\n", msgpost)

	return l
}

func (l *Logger) Infof(format string, v ...any) {
	if l.level >= LogLevelInfo {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.logger.SetPrefix("INFO:  ")
		l.logger.Printf(format, v...)
	}
}

func (l *Logger) Warnf(format string, v ...any) {
	if l.level >= LogLevelWarn {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.logger.SetPrefix("WARN:  ")
		l.logger.Printf(format, v...)
	}
}

func (l *Logger) Errorf(format string, v ...any) {
	if l.level >= LogLevelError {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.logger.SetPrefix("ERROR: ")
		l.logger.Printf(format, v...)
	}
}

func (l *Logger) Debugf(format string, v ...any) {
	if l.level >= LogLevelDebug {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.logger.SetPrefix("DEBUG: ")
		l.logger.Printf(format, v...)
	}
}

func (l *Logger) Interceptf(format string, v ...any) {
	if l.level >= LogLevelIntercept {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.logger.SetPrefix("INTERCEPT: ")
		l.logger.Printf(format, v...)
	}
}
