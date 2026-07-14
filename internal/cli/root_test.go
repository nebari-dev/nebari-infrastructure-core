package cli

import "testing"

func TestNewRootCmdHasExactlyExpectedCommands(t *testing.T) {
	root := NewRootCmd()

	got := make(map[string]bool)
	for _, c := range root.Commands() {
		got[c.Name()] = true
	}

	want := []string{"deploy", "destroy", "validate", "version", "kubeconfig"}
	if len(got) != len(want) {
		t.Fatalf("got %d commands (%v), want %d: %v", len(got), got, len(want), want)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("expected command %q not found in root command tree", name)
		}
	}
}
