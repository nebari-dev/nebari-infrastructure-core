package status

import (
	"context"
	"testing"
	"time"
)

func TestNewStatusUpdate(t *testing.T) {
	tests := []struct {
		name    string
		level   Level
		message string
	}{
		{
			name:    "info level",
			level:   LevelInfo,
			message: "Test info message",
		},
		{
			name:    "error level",
			level:   LevelError,
			message: "Test error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now()
			update := NewStatusUpdate(tt.level, tt.message)
			after := time.Now()

			if update.Level != tt.level {
				t.Errorf("Level = %v, want %v", update.Level, tt.level)
			}
			if update.Message != tt.message {
				t.Errorf("Message = %v, want %v", update.Message, tt.message)
			}
			if update.Timestamp.Before(before) || update.Timestamp.After(after) {
				t.Errorf("Timestamp %v is not between %v and %v", update.Timestamp, before, after)
			}
		})
	}
}

func TestStatusUpdate_WithResource(t *testing.T) {
	update := NewStatusUpdate(LevelInfo, "test").WithResource("vpc")
	if update.Resource != "vpc" {
		t.Errorf("Resource = %v, want %v", update.Resource, "vpc")
	}
}

func TestStatusUpdate_WithAction(t *testing.T) {
	update := NewStatusUpdate(LevelInfo, "test").WithAction("creating")
	if update.Action != "creating" {
		t.Errorf("Action = %v, want %v", update.Action, "creating")
	}
}

func TestStatusUpdate_WithMetadata(t *testing.T) {
	update := NewStatusUpdate(LevelInfo, "test").
		WithMetadata("key1", "value1").
		WithMetadata("key2", 42)

	if len(update.Metadata) != 2 {
		t.Errorf("Metadata length = %d, want 2", len(update.Metadata))
	}
	if update.Metadata["key1"] != "value1" {
		t.Errorf("Metadata[key1] = %v, want value1", update.Metadata["key1"])
	}
	if update.Metadata["key2"] != 42 {
		t.Errorf("Metadata[key2] = %v, want 42", update.Metadata["key2"])
	}
}

func TestStatusUpdate_ChainedBuilders(t *testing.T) {
	update := NewStatusUpdate(LevelProgress, "Creating VPC").
		WithResource("vpc").
		WithAction("creating").
		WithMetadata("vpc_id", "vpc-12345").
		WithMetadata("cidr", "10.0.0.0/16")

	if update.Level != LevelProgress {
		t.Errorf("Level = %v, want %v", update.Level, LevelProgress)
	}
	if update.Message != "Creating VPC" {
		t.Errorf("Message = %v, want 'Creating VPC'", update.Message)
	}
	if update.Resource != "vpc" {
		t.Errorf("Resource = %v, want vpc", update.Resource)
	}
	if update.Action != "creating" {
		t.Errorf("Action = %v, want creating", update.Action)
	}
	if update.Metadata["vpc_id"] != "vpc-12345" {
		t.Errorf("Metadata[vpc_id] = %v, want vpc-12345", update.Metadata["vpc_id"])
	}
}

func TestSend_NoChannel(t *testing.T) {
	// Should not panic when no channel in context
	ctx := context.Background()
	Send(ctx, NewStatusUpdate(LevelInfo, "test"))
	// If we get here without panic, test passes
}

func TestSend_WithChannel(t *testing.T) {
	ch := make(chan StatusUpdate, 10)
	ctx := WithChannel(context.Background(), ch)

	update := NewStatusUpdate(LevelInfo, "test message")
	Send(ctx, update)

	select {
	case received := <-ch:
		if received.Message != "test message" {
			t.Errorf("Received message = %v, want 'test message'", received.Message)
		}
		if received.Level != LevelInfo {
			t.Errorf("Received level = %v, want %v", received.Level, LevelInfo)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for status update")
	}
}

func TestSend_FullChannel(t *testing.T) {
	// Create a channel with buffer size 1
	ch := make(chan StatusUpdate, 1)
	ctx := WithChannel(context.Background(), ch)

	// Fill the channel
	Send(ctx, NewStatusUpdate(LevelInfo, "message 1"))

	// Try to send another - should not block
	done := make(chan bool)
	go func() {
		Send(ctx, NewStatusUpdate(LevelInfo, "message 2"))
		done <- true
	}()

	select {
	case <-done:
		// Success - Send() didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("Send() blocked on full channel")
	}

	// Verify first message is still in channel
	select {
	case msg := <-ch:
		if msg.Message != "message 1" {
			t.Errorf("First message = %v, want 'message 1'", msg.Message)
		}
	default:
		t.Error("Expected message in channel")
	}
}

func TestConvenienceFunctions(t *testing.T) {
	tests := []struct {
		name     string
		sendFunc func(context.Context, string)
		level    Level
	}{
		{"Info", Info, LevelInfo},
		{"Progress", Progress, LevelProgress},
		{"Success", Success, LevelSuccess},
		{"Warning", Warning, LevelWarning},
		{"Error", Error, LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan StatusUpdate, 10)
			ctx := WithChannel(context.Background(), ch)

			tt.sendFunc(ctx, "test message")

			select {
			case received := <-ch:
				if received.Level != tt.level {
					t.Errorf("Level = %v, want %v", received.Level, tt.level)
				}
				if received.Message != "test message" {
					t.Errorf("Message = %v, want 'test message'", received.Message)
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("Timeout waiting for status update")
			}
		})
	}
}

func TestSend_SetsTimestamp(t *testing.T) {
	ch := make(chan StatusUpdate, 10)
	ctx := WithChannel(context.Background(), ch)

	// Send update without timestamp
	update := StatusUpdate{
		Level:   LevelInfo,
		Message: "test",
	}

	before := time.Now()
	Send(ctx, update)
	after := time.Now()

	select {
	case received := <-ch:
		if received.Timestamp.IsZero() {
			t.Error("Timestamp was not set")
		}
		if received.Timestamp.Before(before) || received.Timestamp.After(after) {
			t.Errorf("Timestamp %v is not between %v and %v", received.Timestamp, before, after)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for status update")
	}
}

func TestSend_PreservesExistingTimestamp(t *testing.T) {
	ch := make(chan StatusUpdate, 10)
	ctx := WithChannel(context.Background(), ch)

	// Send update with timestamp already set
	timestamp := time.Now().Add(-1 * time.Hour)
	update := StatusUpdate{
		Level:     LevelInfo,
		Message:   "test",
		Timestamp: timestamp,
	}

	Send(ctx, update)

	select {
	case received := <-ch:
		if !received.Timestamp.Equal(timestamp) {
			t.Errorf("Timestamp = %v, want %v", received.Timestamp, timestamp)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for status update")
	}
}

func TestLevelConstants(t *testing.T) {
	if LevelInfo != "info" {
		t.Errorf("LevelInfo = %v, want 'info'", LevelInfo)
	}
	if LevelProgress != "progress" {
		t.Errorf("LevelProgress = %v, want 'progress'", LevelProgress)
	}
	if LevelSuccess != "success" {
		t.Errorf("LevelSuccess = %v, want 'success'", LevelSuccess)
	}
	if LevelWarning != "warning" {
		t.Errorf("LevelWarning = %v, want 'warning'", LevelWarning)
	}
	if LevelError != "error" {
		t.Errorf("LevelError = %v, want 'error'", LevelError)
	}
}

func TestFormattedFunctions(t *testing.T) {
	tests := []struct {
		name     string
		sendFunc func(context.Context, string, ...interface{})
		level    Level
	}{
		{"Infof", Infof, LevelInfo},
		{"Progressf", Progressf, LevelProgress},
		{"Successf", Successf, LevelSuccess},
		{"Warningf", Warningf, LevelWarning},
		{"Errorf", Errorf, LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan StatusUpdate, 10)
			ctx := WithChannel(context.Background(), ch)

			tt.sendFunc(ctx, "test message with %s and %d", "string", 42)

			select {
			case received := <-ch:
				if received.Level != tt.level {
					t.Errorf("Level = %v, want %v", received.Level, tt.level)
				}
				expectedMessage := "test message with string and 42"
				if received.Message != expectedMessage {
					t.Errorf("Message = %v, want %v", received.Message, expectedMessage)
				}
			case <-time.After(100 * time.Millisecond):
				t.Error("Timeout waiting for status update")
			}
		})
	}
}

func TestSendf(t *testing.T) {
	ch := make(chan StatusUpdate, 10)
	ctx := WithChannel(context.Background(), ch)

	Sendf(ctx, LevelInfo, "VPC %s has %d subnets", "vpc-12345", 4)

	select {
	case received := <-ch:
		if received.Level != LevelInfo {
			t.Errorf("Level = %v, want %v", received.Level, LevelInfo)
		}
		expectedMessage := "VPC vpc-12345 has 4 subnets"
		if received.Message != expectedMessage {
			t.Errorf("Message = %v, want %v", received.Message, expectedMessage)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timeout waiting for status update")
	}
}
