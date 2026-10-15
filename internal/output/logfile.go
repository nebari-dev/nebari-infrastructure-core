package output

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LogFilePath returns where a command run writes its log by default:
// ~/.nic/logs/<project>/<UTC start time>-<command>.jsonl. project must be a
// single path element: callers check it with config.IsSafeProjectName, since
// the log opens before the config is validated.
func LogFilePath(home, project, command string, start time.Time) string {
	name := start.UTC().Format("20060102T150405Z") + "-" + command + ".jsonl"
	return filepath.Join(home, ".nic", "logs", project, name)
}

// OpenLogFile creates path for writing, with its parent directories. Logs can
// hold infrastructure identifiers, so the file is readable by its owner only.
func OpenLogFile(path string) (*os.File, error) {
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	return f, nil
}
