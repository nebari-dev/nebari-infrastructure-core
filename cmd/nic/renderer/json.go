package renderer

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// JSON is a Renderer that outputs structured JSON log lines via slog.
// It preserves the original NIC output behavior for scripts and log aggregators.
type JSON struct {
	logger *slog.Logger
}

// NewJSON creates a JSON renderer that writes structured log lines to w.
func NewJSON(w io.Writer) *JSON {
	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	return &JSON{logger: logger}
}

func (j *JSON) StartPhase(name string) {
	j.logger.Info("Phase started", "phase", name)
}

func (j *JSON) EndPhase(status PhaseStatus, elapsed time.Duration) {
	s := "ok"
	if status == PhaseFailed {
		s = "failed"
	}
	j.logger.Info("Phase completed", "status", s, "duration", elapsed.String())
}

func (j *JSON) StartStep(name string) {
	j.logger.Info("Step started", "step", name)
}

func (j *JSON) EndStep(status StepStatus, elapsed time.Duration, detail string) {
	s := "ok"
	switch status {
	case StepFailed:
		s = "failed"
	case StepSkipped:
		s = "skipped"
	case StepWarning:
		s = "warning"
	}
	attrs := []any{"status", s, "duration", elapsed.String()}
	if detail != "" {
		attrs = append(attrs, "detail", detail)
	}
	j.logger.Info("Step completed", attrs...)
}

func (j *JSON) Detail(line string) {
	j.logger.Info("Detail", "output", line)
}

func (j *JSON) Summary(items []SummaryItem) {
	for _, item := range items {
		j.logger.Info("Summary", "label", item.Label, "value", item.Value)
	}
}

func (j *JSON) Error(err error, hint string) {
	attrs := []any{"error", err.Error()}
	if hint != "" {
		attrs = append(attrs, "hint", hint)
	}
	j.logger.Error("Error", attrs...)
}

func (j *JSON) Warn(message string) {
	j.logger.Warn("Warning", "message", message)
}

func (j *JSON) Info(message string) {
	j.logger.Info("Info", "message", message)
}

func (j *JSON) Version(ver, commitHash, tofuVer string, clusterProviders, dnsProviders []string) {
	j.logger.Info("Version",
		"version", ver,
		"commit", commitHash,
		"opentofu_version", tofuVer,
		"cluster_providers", clusterProviders,
		"dns_providers", dnsProviders,
	)
}

func (j *JSON) Confirm(message string, details map[string]string, expected string) (bool, error) {
	// In JSON mode we still need to interact with the user via stdin/stdout
	fmt.Println(message)
	for k, v := range details {
		fmt.Printf("  %s: %s\n", k, v)
	}
	fmt.Printf("Type '%s' to confirm: ", expected)

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("failed to read user input: %w", err)
	}
	return strings.TrimSpace(response) == expected, nil
}

func (j *JSON) DetailWriter() io.Writer {
	return &lineWriter{r: j}
}

// Logger returns the underlying slog.Logger so it can be set as the default.
func (j *JSON) Logger() *slog.Logger {
	return j.logger
}
