package status

import (
	"context"
	"fmt"
	"time"
)

// Level represents the severity level of a status update
type Level string

const (
	// LevelInfo represents informational status updates
	LevelInfo Level = "info"

	// LevelProgress represents progress updates during operations
	LevelProgress Level = "progress"

	// LevelSuccess represents successful completion of operations
	LevelSuccess Level = "success"

	// LevelWarning represents warnings that don't prevent operation
	LevelWarning Level = "warning"

	// LevelError represents error conditions
	LevelError Level = "error"
)

// Update represents a status update message that can be sent through the status channel
type Update struct {
	// Level is the severity level of this status update
	Level Level

	// Message is the human-readable status message
	Message string

	// Resource is the type of resource being operated on (e.g., "vpc", "nat-gateway", "eks-cluster")
	Resource string

	// Action is the action being performed (e.g., "creating", "updating", "deleting", "discovering")
	Action string

	// Metadata contains optional additional structured data about the status
	Metadata map[string]interface{}

	// Timestamp is when this status update was created
	Timestamp time.Time
}

// NewUpdate creates a new Update with the current timestamp
func NewUpdate(level Level, message string) Update {
	return Update{
		Level:     level,
		Message:   message,
		Timestamp: time.Now(),
	}
}

// WithResource adds resource information to the status update
func (s Update) WithResource(resource string) Update {
	s.Resource = resource
	return s
}

// WithAction adds action information to the status update
func (s Update) WithAction(action string) Update {
	s.Action = action
	return s
}

// WithMetadata adds metadata to the status update
func (s Update) WithMetadata(key string, value interface{}) Update {
	if s.Metadata == nil {
		s.Metadata = make(map[string]interface{})
	}
	s.Metadata[key] = value
	return s
}

// Send sends a status update through the channel stored in the context (if present)
// This function is non-blocking and will drop the message if the channel is full
func Send(ctx context.Context, update Update) {
	ch := getChannel(ctx)
	if ch == nil {
		// No status channel in context - silently skip
		return
	}

	// Set timestamp if not already set
	if update.Timestamp.IsZero() {
		update.Timestamp = time.Now()
	}

	// Non-blocking send - drop message if channel is full
	select {
	case ch <- update:
		// Message sent successfully
	default:
		// Channel full - drop message
		// In production, you might want to log this or use a metric
	}
}

// Sendf sends a formatted status update message
func Sendf(ctx context.Context, level Level, format string, args ...interface{}) {
	Send(ctx, Update{
		Level:     level,
		Message:   fmt.Sprintf(format, args...),
		Timestamp: time.Now(),
	})
}

// Info sends an informational status update
func Info(ctx context.Context, message string) {
	Send(ctx, NewUpdate(LevelInfo, message))
}

// Infof sends a formatted informational status update
func Infof(ctx context.Context, format string, args ...interface{}) {
	Sendf(ctx, LevelInfo, format, args...)
}

// Progress sends a progress status update
func Progress(ctx context.Context, message string) {
	Send(ctx, NewUpdate(LevelProgress, message))
}

// Progressf sends a formatted progress status update
func Progressf(ctx context.Context, format string, args ...interface{}) {
	Sendf(ctx, LevelProgress, format, args...)
}

// Success sends a success status update
func Success(ctx context.Context, message string) {
	Send(ctx, NewUpdate(LevelSuccess, message))
}

// Successf sends a formatted success status update
func Successf(ctx context.Context, format string, args ...interface{}) {
	Sendf(ctx, LevelSuccess, format, args...)
}

// Warning sends a warning status update
func Warning(ctx context.Context, message string) {
	Send(ctx, NewUpdate(LevelWarning, message))
}

// Warningf sends a formatted warning status update
func Warningf(ctx context.Context, format string, args ...interface{}) {
	Sendf(ctx, LevelWarning, format, args...)
}

// Error sends an error status update
func Error(ctx context.Context, message string) {
	Send(ctx, NewUpdate(LevelError, message))
}

// Errorf sends a formatted error status update
func Errorf(ctx context.Context, format string, args ...interface{}) {
	Sendf(ctx, LevelError, format, args...)
}
