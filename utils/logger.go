// Logger provides a simple logging interface
package utils

import (
	"fmt"
	"log"
	"os"
)

// Logger is a simple wrapper around the standard log.Logger
type Logger struct {
	logger *log.Logger
}

// NewLogger creates a new Logger instance
func NewLogger(prefix string) *Logger {
	return &Logger{
		logger: log.New(os.Stdout, fmt.Sprintf("[%s] ", prefix), log.LstdFlags),
	}
}

// Info logs an informational message
func (l *Logger) Info(format string, v ...interface{}) {
	l.logger.Printf("INFO: "+format, v...)
}

// Warning logs a warning message
func (l *Logger) Warning(format string, v ...interface{}) {
	l.logger.Printf("WARNING: "+format, v...)
}

// Error logs an error message
func (l *Logger) Error(format string, v ...interface{}) {
	l.logger.Printf("ERROR: "+format, v...)
}

// Debug logs a debug message
func (l *Logger) Debug(format string, v ...interface{}) {
	l.logger.Printf("DEBUG: "+format, v...)
}

// Success logs a success message
func (l *Logger) Success(format string, v ...interface{}) {
	l.logger.Printf("SUCCESS: "+format, v...)
}

// Global logger instance
var (
	Info    = NewLogger("INFO").Info
	Warning = NewLogger("WARNING").Warning
	Error   = NewLogger("ERROR").Error
	Debug   = NewLogger("DEBUG").Debug
	Success = NewLogger("SUCCESS").Success
)

