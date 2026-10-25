// Logger provides a simple logging interface
package utils

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// LogFilePath is where all log output is mirrored (relative to the working directory,
// like the rest of the storage directory).
var LogFilePath = filepath.Join("storage", "logs", "acrynite.log")

var (
	logFile     *os.File
	logFileOnce sync.Once
	logFileMu   sync.Mutex
)

// fileLogWriter appends to the log file, opening it lazily. It never fails the caller:
// if the file can't be opened, output only goes to stdout.
type fileLogWriter struct{}

func (fileLogWriter) Write(p []byte) (int, error) {
	logFileOnce.Do(func() {
		if err := os.MkdirAll(filepath.Dir(LogFilePath), 0755); err != nil {
			return
		}
		f, err := os.OpenFile(LogFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			logFile = f
		}
	})
	logFileMu.Lock()
	defer logFileMu.Unlock()
	if logFile != nil {
		_, _ = logFile.Write(p)
	}
	return len(p), nil
}

// Logger is a simple wrapper around the standard log.Logger
type Logger struct {
	logger *log.Logger
}

// NewLogger creates a new Logger instance that writes to stdout and the log file
func NewLogger(prefix string) *Logger {
	return &Logger{
		logger: log.New(io.MultiWriter(os.Stdout, fileLogWriter{}), fmt.Sprintf("[%s] ", prefix), log.LstdFlags),
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

