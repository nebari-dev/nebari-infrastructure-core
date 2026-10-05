package local

import (
	"context"
	"strings"
	"testing"
)

func TestConfigValidate(t *testing.T) {
	mount := func(host, container string) KindMount {
		return KindMount{HostPath: host, ContainerPath: container}
	}

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{name: "zero config is valid", cfg: Config{}},
		{name: "empty kind block is valid", cfg: Config{Kind: &KindConfig{}}},
		{
			name: "node groups with image, labels, and mounts are valid",
			cfg: Config{Kind: &KindConfig{
				ExtraMounts: []KindMount{mount("/tmp/data", "/data")},
				NodeGroups: map[string]KindNodeGroup{
					"general": {Count: 2},
					"infra": {
						Count:       1,
						Image:       "kindest/node:v1.32.2",
						Labels:      map[string]string{"dedicated": "infra", "example.com/tier": "infra"},
						ExtraMounts: []KindMount{mount("/tmp/models", "/models")},
					},
				},
			}},
		},
		{
			// The kubelet may set labels in these namespaces itself.
			name: "kubelet-settable kubernetes.io labels are valid",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"general": {Count: 1, Labels: map[string]string{"node.kubernetes.io/pool": "general"}},
			}}},
		},
		{
			name: "zero count is rejected",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"general": {Count: 0},
			}}},
			wantErr: `node_groups["general"].count must be at least 1`,
		},
		{
			name: "negative count is rejected",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"general": {Count: -1},
			}}},
			wantErr: `node_groups["general"].count must be at least 1`,
		},
		{
			name: "group name that is not a DNS label is rejected",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"General_Pool": {Count: 1},
			}}},
			wantErr: `node_groups["General_Pool"]: name must be a lowercase DNS label`,
		},
		{
			// The kubelet refuses to start with such a label, so the node
			// would never register and the deploy would only time out.
			name: "reserved kubernetes.io label is rejected",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"infra": {Count: 1, Labels: map[string]string{"node-role.kubernetes.io/infra": ""}},
			}}},
			wantErr: `node_groups["infra"].labels: "node-role.kubernetes.io/infra" is reserved`,
		},
		{
			name: "reserved k8s.io label is rejected",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"infra": {Count: 1, Labels: map[string]string{"example.k8s.io/pool": "infra"}},
			}}},
			wantErr: "is reserved",
		},
		{
			name: "label NIC sets itself is rejected",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"infra": {Count: 1, Labels: map[string]string{nodeGroupLabel: "other"}},
			}}},
			wantErr: "is set by NIC",
		},
		{
			name: "invalid label key is rejected",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"infra": {Count: 1, Labels: map[string]string{"bad key": "x"}},
			}}},
			wantErr: `node_groups["infra"].labels: invalid key "bad key"`,
		},
		{
			name: "invalid label value is rejected",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"infra": {Count: 1, Labels: map[string]string{"tier": "not valid!"}},
			}}},
			wantErr: `node_groups["infra"].labels: invalid value "not valid!"`,
		},
		{
			name: "relative shared mount path is rejected",
			cfg: Config{Kind: &KindConfig{
				ExtraMounts: []KindMount{mount("data", "/data")},
			}},
			wantErr: "kind extra_mounts paths must be absolute",
		},
		{
			name: "relative group mount path is rejected",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"infra": {Count: 1, ExtraMounts: []KindMount{mount("/tmp/models", "models")}},
			}}},
			wantErr: `node_groups["infra"].extra_mounts paths must be absolute`,
		},
		{
			name: "group mount reusing a shared container path is rejected",
			cfg: Config{Kind: &KindConfig{
				ExtraMounts: []KindMount{mount("/tmp/data", "/data")},
				NodeGroups: map[string]KindNodeGroup{
					"infra": {Count: 1, ExtraMounts: []KindMount{mount("/tmp/other", "/data")}},
				},
			}},
			wantErr: `node_groups["infra"].extra_mounts: container_path /data is mounted more than once`,
		},
		{
			name: "duplicate shared container path is rejected",
			cfg: Config{Kind: &KindConfig{
				ExtraMounts: []KindMount{mount("/tmp/a", "/data"), mount("/tmp/b", "/data")},
			}},
			wantErr: "kind extra_mounts: container_path /data is mounted more than once",
		},
		{
			// Groups are checked in name order, so the first error is stable.
			name: "several invalid groups report the first by name",
			cfg: Config{Kind: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"zeta":  {Count: 0},
				"alpha": {Count: 0},
			}}},
			wantErr: `node_groups["alpha"]`,
		},
		{
			name:    "https_port above the port range is rejected",
			cfg:     Config{HTTPSPort: 99999999},
			wantErr: "https_port must be between 1 and 65535",
		},
		{
			name:    "negative http_port is rejected",
			cfg:     Config{HTTPPort: -1},
			wantErr: "http_port must be between 1 and 65535",
		},
		{
			name:    "both ports invalid reports http_port first",
			cfg:     Config{HTTPPort: -1, HTTPSPort: 99999999},
			wantErr: "http_port must be between 1 and 65535",
		},
		{name: "valid custom ports pass", cfg: Config{HTTPPort: 8080, HTTPSPort: 8443}},
		{
			name:    "equal ports are rejected",
			cfg:     Config{HTTPPort: 8443, HTTPSPort: 8443},
			wantErr: "http_port and https_port must differ",
		},
		{
			name:    "http_port colliding with the default https_port is rejected",
			cfg:     Config{HTTPPort: 443},
			wantErr: "http_port and https_port must differ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate(context.Background())
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestKindConfigWorkerCount(t *testing.T) {
	tests := []struct {
		name string
		cfg  *KindConfig
		want int
	}{
		{name: "nil kind block", cfg: nil, want: 0},
		{name: "no node groups", cfg: &KindConfig{}, want: 0},
		{
			name: "counts summed across groups",
			cfg: &KindConfig{NodeGroups: map[string]KindNodeGroup{
				"general": {Count: 2},
				"infra":   {Count: 1},
			}},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.WorkerCount(); got != tt.want {
				t.Errorf("WorkerCount() = %d, want %d", got, tt.want)
			}
		})
	}
}
