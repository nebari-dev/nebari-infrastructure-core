package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
)

func TestPrintDNSGuidance(t *testing.T) {
	tests := []struct {
		name         string
		domain       string
		endpoint     *endpoint.LoadBalancerEndpoint
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:   "with hostname endpoint shows CNAME record",
			domain: "example.com",
			endpoint: &endpoint.LoadBalancerEndpoint{
				Hostname: "abc123.us-west-2.elb.amazonaws.com",
			},
			wantContains: []string{
				"CNAME",
				"abc123.us-west-2.elb.amazonaws.com",
				"example.com",
				"*.example.com",
			},
			wantAbsent: []string{
				"ingress-nginx",
				"kubectl",
				"<load-balancer-endpoint>",
			},
		},
		{
			name:   "with IP endpoint shows A record",
			domain: "example.com",
			endpoint: &endpoint.LoadBalancerEndpoint{
				IP: "34.102.136.180",
			},
			wantContains: []string{
				"A",
				"34.102.136.180",
				"example.com",
			},
			wantAbsent: []string{
				"ingress-nginx",
				"kubectl",
			},
		},
		{
			name:     "with nil endpoint shows fallback instructions",
			domain:   "example.com",
			endpoint: nil,
			wantContains: []string{
				"example.com",
				"nic status",
			},
			wantAbsent: []string{
				"ingress-nginx",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.NebariConfig{Domain: tt.domain}

			output := captureStdout(func() {
				printDNSGuidance(cfg, tt.endpoint)
			})

			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("output should contain %q, got:\n%s", want, output)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(output, absent) {
					t.Errorf("output should NOT contain %q, got:\n%s", absent, output)
				}
			}
		})
	}
}

func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}
