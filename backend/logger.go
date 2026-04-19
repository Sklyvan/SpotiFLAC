package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	appLogFile *os.File
	appLogMu   sync.Mutex
)

// InitAppLogger opens (or creates) ~/.spotiflac/debug.log.
// Called once from app startup.
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
	AppLog("=== Session started (%s) — log: %s ===\n", time.Now().Format("2006-01-02 15:04:05"), logPath)
}

// CloseAppLogger flushes and closes the log file. Called from app shutdown.
func CloseAppLogger() {
	appLogMu.Lock()
	defer appLogMu.Unlock()
	if appLogFile != nil {
		_ = appLogFile.Sync()
		_ = appLogFile.Close()
		appLogFile = nil
	}
}

// AppLog writes a timestamped line to the log file (always) and to stdout (best-effort).
// On Windows GUI builds stdout is not attached to a console, so we write to each
// destination independently — a stdout failure does NOT block the file write.
func AppLog(format string, args ...interface{}) {
	appLogMu.Lock()
	defer appLogMu.Unlock()

	ts := time.Now().Format("15:04:05.000")
	line := fmt.Sprintf("[%s] %s", ts, fmt.Sprintf(format, args...))

	// stdout: ignore errors (detached in GUI builds)
	_, _ = fmt.Fprint(os.Stdout, line)

	// file: always attempt, sync immediately so data survives crashes
	if appLogFile != nil {
		_, _ = fmt.Fprint(appLogFile, line)
		_ = appLogFile.Sync()
	}
}
