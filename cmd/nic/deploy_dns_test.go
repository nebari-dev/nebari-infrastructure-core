package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/cmd/nic/renderer"
)

func TestRendererSummary(t *testing.T) {
	tests := []struct {
		name         string
		items        []renderer.SummaryItem
		wantContains []string
	}{
		{
			name: "shows service endpoints",
			items: []renderer.SummaryItem{
				{Label: "ArgoCD", Value: "https://argocd.example.com"},
				{Label: "Keycloak", Value: "https://keycloak.example.com"},
			},
			wantContains: []string{
				"ArgoCD",
				"https://argocd.example.com",
				"Keycloak",
				"https://keycloak.example.com",
			},
		},
		{
			name:         "empty summary produces no output",
			items:        nil,
			wantContains: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := renderer.NewPretty(&buf, false)
			r.Summary(tt.items)
			output := buf.String()

			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("output should contain %q, got:\n%s", want, output)
				}
			}
		})
	}
}
