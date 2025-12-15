package local

// Config represents local K3s configuration
type Config struct {
	KubeContext      string                       `yaml:"kube_context,omitempty"`
	NodeSelectors    map[string]map[string]string `yaml:"node_selectors,omitempty"`
	AdditionalFields map[string]any               `yaml:",inline"`
}
