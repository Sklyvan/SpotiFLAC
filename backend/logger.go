package backend

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	appLogFile   *os.File
	appLogWriter io.Writer
	appLogMu     sync.Mutex
)

// InitAppLogger opens (or creates) ~/.spotiflac/debug.log and sets up the writer
// used by AppLog. Called once from app startup.
func InitAppLogger() {
	appDir, err := EnsureAppDir()
	if err != nil {
		fmt.Printf("Warning: could not init app logger: %v\n", err)
		return
	}

	logPath := filepath.Join(appDir, "debug.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Printf("Warning: could not open log file %s: %v\n", logPath, err)
		return
	}

	appLogFile = f
	appLogWriter = io.MultiWriter(os.Stdout, f)
	AppLog("Logger initialised — writing to %s\n", logPath)
}

// CloseAppLogger flushes and closes the log file. Called from app shutdown.
func CloseAppLogger() {
	appLogMu.Lock()
	defer appLogMu.Unlock()
	if appLogFile != nil {
		_ = appLogFile.Sync()
		_ = appLogFile.Close()
		appLogFile = nil
		appLogWriter = nil
	}
}

// AppLog writes a timestamped line to stdout and to ~/.spotiflac/debug.log.
func AppLog(format string, args ...interface{}) {
	appLogMu.Lock()
	defer appLogMu.Unlock()

	ts := time.Now().Format("15:04:05.000")
	line := fmt.Sprintf("[%s] %s", ts, fmt.Sprintf(format, args...))

	w := appLogWriter
	if w == nil {
		w = os.Stdout
	}
	fmt.Fprint(w, line)
}
