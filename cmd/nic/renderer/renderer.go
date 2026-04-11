package renderer

import (
	"io"
	"time"
)

// PhaseStatus represents the outcome of a deployment phase.
type PhaseStatus int

const (
	PhaseOK PhaseStatus = iota
	PhaseFailed
)

// StepStatus represents the outcome of an individual step within a phase.
type StepStatus int

const (
	StepOK StepStatus = iota
	StepFailed
	StepSkipped
	StepWarning
)

// SummaryItem is a label-value pair displayed in the final summary box.
type SummaryItem struct {
	Label string
	Value string
}

// Renderer controls how NIC's CLI output is presented to the user.
// Three implementations exist: JSON (machine-readable), Pretty (colored Unicode
// for TTYs), and Plain (ASCII-only, no color for dumb terminals).
type Renderer interface {
	// StartPhase begins a named deployment phase (e.g., "Infrastructure", "ArgoCD").
	StartPhase(name string)
	// EndPhase closes the current phase with a status and elapsed duration.
	EndPhase(status PhaseStatus, elapsed time.Duration)

	// StartStep begins a named step within the current phase.
	StartStep(name string)
	// EndStep closes the current step. detail is optional extra info shown inline.
	EndStep(status StepStatus, elapsed time.Duration, detail string)

	// Detail emits a sub-detail line (third-party tool output, verbose info).
	// Hidden by default in pretty/plain mode; shown with --verbose.
	Detail(line string)

	// Summary prints a final summary box with endpoints, credentials, etc.
	Summary(items []SummaryItem)

	// Error prints a formatted error with an optional hint for the user.
	Error(err error, hint string)

	// Warn prints a warning message.
	Warn(message string)

	// Info prints an informational message outside of any phase/step context.
	Info(message string)

	// Version prints version information.
	Version(version, commit, tofuVersion string, clusterProviders, dnsProviders []string)

	// Confirm prompts the user to type the expected string to confirm a destructive action.
	Confirm(message string, details map[string]string, expected string) (bool, error)

	// DetailWriter returns an io.Writer that splits incoming bytes on newlines
	// and routes each line to Detail(). Used to capture third-party tool stdout/stderr.
	DetailWriter() io.Writer
}
