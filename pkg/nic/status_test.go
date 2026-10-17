package nic

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

func TestSlogHandler(t *testing.T) {
	tests := []struct {
		name    string
		updates []status.Update
		// want holds, per emitted record, the expected level, msg and phase
		// attribute ("" means the record has no phase attribute).
		want []struct{ level, msg, phase string }
	}{
		{
			name: "records before any phase carry no phase",
			updates: []status.Update{
				status.NewUpdate(status.LevelInfo, "Starting deployment"),
			},
			want: []struct{ level, msg, phase string }{
				{"INFO", "Starting deployment", ""},
			},
		},
		{
			name: "records are tagged with the latest phase",
			updates: []status.Update{
				status.NewUpdate(status.LevelProgress, PhaseInfrastructure).WithResource(status.ResourcePhase).WithMetadata(status.MetadataKeyPhase, PhaseInfrastructure),
				status.NewUpdate(status.LevelSuccess, "Infrastructure deployment completed"),
				status.NewUpdate(status.LevelProgress, PhaseArgoCD).WithResource(status.ResourcePhase).WithMetadata(status.MetadataKeyPhase, PhaseArgoCD),
				status.NewUpdate(status.LevelWarning, "Failed to install Argo CD"),
				status.NewUpdate(status.LevelError, "Failed to generate secret"),
			},
			want: []struct{ level, msg, phase string }{
				{"INFO", PhaseInfrastructure, PhaseInfrastructure},
				{"INFO", "Infrastructure deployment completed", PhaseInfrastructure},
				{"INFO", PhaseArgoCD, PhaseArgoCD},
				{"WARN", "Failed to install Argo CD", PhaseArgoCD},
				{"ERROR", "Failed to generate secret", PhaseArgoCD},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			handle := SlogHandler(slog.New(slog.NewJSONHandler(&buf, nil)))
			for _, u := range tt.updates {
				handle(u)
			}

			dec := json.NewDecoder(&buf)
			for i, want := range tt.want {
				var rec map[string]any
				if err := dec.Decode(&rec); err != nil {
					t.Fatalf("record %d: %v", i, err)
				}
				if rec["level"] != want.level || rec["msg"] != want.msg {
					t.Errorf("record %d = %v %q, want %v %q", i, rec["level"], rec["msg"], want.level, want.msg)
				}
				got, has := rec[status.MetadataKeyPhase]
				if want.phase == "" && has {
					t.Errorf("record %d has phase %v, want none", i, got)
				}
				if want.phase != "" && got != want.phase {
					t.Errorf("record %d phase = %v, want %q", i, got, want.phase)
				}
			}
			if dec.More() {
				t.Errorf("more records than expected")
			}
		})
	}
}
