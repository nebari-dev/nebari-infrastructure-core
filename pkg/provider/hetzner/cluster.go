package hetzner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

// clusterParams holds all parameters needed to generate a hetzner-k3s cluster.yaml.
type clusterParams struct {
	ClusterName    string
	K3sVersion     string
	HetznerToken   string
	SSHPublicKey   string
	SSHPrivateKey  string
	KubeconfigPath string
	Config         Config
}

const clusterTemplate = `cluster_name: {{ .ClusterName }}
kubeconfig_path: "{{ .KubeconfigPath }}"
k3s_version: {{ .K3sVersion }}
hetzner_token: {{ .HetznerToken }}

networking:
  ssh:
    port: 22
    use_agent: false
    public_key_path: "{{ .SSHPublicKey }}"
    private_key_path: "{{ .SSHPrivateKey }}"
  allowed_networks:
    ssh:
      - 0.0.0.0/0
    api:
      - 0.0.0.0/0
  public_network:
    ipv4: true
    ipv6: true
  private_network:
    enabled: true
    subnet: 10.0.0.0/16
  cni:
    enabled: true
    mode: flannel

schedule_workloads_on_masters: false

masters_pool:
  instance_type: {{ .Config.MastersPool.InstanceType }}
  instance_count: {{ .Config.MastersPool.InstanceCount }}
  locations:
    - {{ .Config.Location }}

worker_node_pools:
{{- range .Config.WorkerNodePools }}
  - name: {{ .Name }}
    instance_type: {{ .InstanceType }}
    instance_count: {{ .InstanceCount }}
    location: {{ workerLocation . $.Config.Location }}
{{- if and .Autoscaling .Autoscaling.Enabled }}
    autoscaling:
      enabled: true
      min_instances: {{ .Autoscaling.MinInstances }}
      max_instances: {{ .Autoscaling.MaxInstances }}
{{- end }}
{{- end }}

addons:
  traefik:
    enabled: false
  servicelb:
    enabled: false
  metrics_server:
    enabled: true
  csi_driver:
    enabled: true
  cloud_controller_manager:
    enabled: true
  system_upgrade_controller:
    enabled: true
  cluster_autoscaler:
    enabled: {{ hasAutoscaling .Config.WorkerNodePools }}
  embedded_registry_mirror:
    enabled: true
`

// generateClusterYAML renders the hetzner-k3s cluster.yaml from parameters.
func generateClusterYAML(params clusterParams) (string, error) {
	funcMap := template.FuncMap{
		"workerLocation": func(pool WorkerNodePool, defaultLoc string) string {
			if pool.Location != "" {
				return pool.Location
			}
			return defaultLoc
		},
		"hasAutoscaling": func(pools []WorkerNodePool) bool {
			for _, p := range pools {
				if p.Autoscaling != nil && p.Autoscaling.Enabled {
					return true
				}
			}
			return false
		},
	}

	tmpl, err := template.New("cluster").Funcs(funcMap).Parse(clusterTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse cluster template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("failed to render cluster template: %w", err)
	}

	return buf.String(), nil
}

// runHetznerK3s executes the hetzner-k3s binary with the given subcommand and cluster config.
func runHetznerK3s(ctx context.Context, binaryPath, subcommand, clusterYAMLPath string) error {
	cmd := exec.CommandContext(ctx, binaryPath, subcommand, "--config", clusterYAMLPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hetzner-k3s %s failed: %w", subcommand, err)
	}
	return nil
}

// writeClusterYAML writes the generated cluster.yaml to a directory and returns its path.
func writeClusterYAML(workDir string, content string) (string, error) {
	path := filepath.Join(workDir, "cluster.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("failed to write cluster.yaml: %w", err)
	}
	return path, nil
}
