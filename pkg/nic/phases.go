package nic

// Phase names Deploy and Destroy announce with status.Phase. Every status
// Update sent between two phase markers belongs to the earlier phase, which
// is how the CLI groups its output. They are part of the status stream that
// library consumers see, so treat a rename as a visible change.
const (
	PhaseConfiguration  = "Configuration"
	PhaseInfrastructure = "Infrastructure"
	PhaseGitOps         = "GitOps"
	PhaseArgoCD         = "Argo CD"
	PhaseFoundational   = "Foundational services"
	PhaseEndpoint       = "Endpoint"
	PhaseDNS            = "DNS"
)
