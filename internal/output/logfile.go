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

// OpenLogFile opens path for appending, creating it and its parent
// directories. Logs can hold infrastructure identifiers, so a regular file is
// made readable by its owner only, even when it already existed; directories NIC
// creates are owner-only too, while an existing directory keeps its mode.
func OpenLogFile(path string) (*os.File, error) {
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	// Only a regular file: --log-file can also name a device such as
	// /dev/stderr, whose mode isn't ours to change.
	if info, err := f.Stat(); err == nil && info.Mode().IsRegular() {
		if err := f.Chmod(0o600); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("restrict log file permissions: %w", err)
		}
	}
	return f, nil
}
