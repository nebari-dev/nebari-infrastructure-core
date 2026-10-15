package status

import (
	"context"
	"testing"
)

func TestPhase(t *testing.T) {
	ch := make(chan Update, 1)
	ctx := WithChannel(context.Background(), ch)

	Phase(ctx, "Infrastructure")

	got := <-ch
	name, ok := PhaseName(got)
	if !ok || name != "Infrastructure" {
		t.Fatalf("PhaseName(Phase(...)) = %q, %v; want %q, true", name, ok, "Infrastructure")
	}
	if got.Level != LevelProgress {
		t.Errorf("Level = %v, want %v", got.Level, LevelProgress)
	}
}

func TestPhaseName(t *testing.T) {
	tests := []struct {
		name     string
		update   Update
		wantName string
		wantOK   bool
	}{
		{
			name:     "phase marker",
			update:   NewUpdate(LevelProgress, "GitOps").WithResource(ResourcePhase).WithMetadata(MetadataKeyPhase, "GitOps"),
			wantName: "GitOps",
			wantOK:   true,
		},
		{
			name:   "ordinary update",
			update: NewUpdate(LevelInfo, "Provider selected").WithResource("provider"),
		},
		{
			name:   "phase metadata on a non-phase resource",
			update: NewUpdate(LevelInfo, "x").WithResource("argocd").WithMetadata(MetadataKeyPhase, "Argo CD"),
		},
		{
			name:   "phase resource without a name",
			update: NewUpdate(LevelProgress, "x").WithResource(ResourcePhase),
		},
		{
			name:   "phase name of the wrong type",
			update: NewUpdate(LevelProgress, "x").WithResource(ResourcePhase).WithMetadata(MetadataKeyPhase, 3),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, ok := PhaseName(tt.update)
			if name != tt.wantName || ok != tt.wantOK {
				t.Errorf("PhaseName() = %q, %v; want %q, %v", name, ok, tt.wantName, tt.wantOK)
			}
		})
	}
}
