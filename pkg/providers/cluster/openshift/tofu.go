package openshift

import "embed"

// Embed all files in the templates directory, including dotfiles
// (i.e. .terraform.lock.hcl) so provider versions are pinned.
//
//go:embed all:templates
var tofuTemplates embed.FS

// TFVars are the OpenTofu input variables for the ROSA HCP templates. Field tags
// match the variable names in templates/variables.tf; the struct is marshaled to
// terraform.tfvars.json by tofu.Setup. Empty/zero values are omitted so the
// module/variable defaults apply.
type TFVars struct {
	Region             string   `json:"region"`
	ClusterName        string   `json:"cluster_name"`
	OpenShiftVersion   string   `json:"openshift_version,omitempty"`
	MachineCIDR        string   `json:"machine_cidr,omitempty"`
	AvailabilityZones  []string `json:"availability_zones,omitempty"`
	ComputeMachineType string   `json:"compute_machine_type,omitempty"`
	Replicas           int      `json:"replicas,omitempty"`
}

// toTFVars maps the provider config to OpenTofu variables. projectName becomes
// the cluster name, mirroring the aws provider.
func (c *Config) toTFVars(projectName string) (*TFVars, error) {
	return &TFVars{
		Region:             c.Region,
		ClusterName:        projectName,
		OpenShiftVersion:   c.OpenShiftVersion,
		MachineCIDR:        c.MachineCIDR,
		AvailabilityZones:  c.AvailabilityZones,
		ComputeMachineType: c.Compute.InstanceType,
		Replicas:           c.Compute.Replicas,
	}, nil
}
