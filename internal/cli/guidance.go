package cli

import (
	"fmt"
	"net"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/output"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

// platformURL is the address a successful deploy is served on, for the
// result line: https://<domain>, with the HTTPS port when it is not 443.
// Empty without a domain or in a dry run.
func platformURL(cfg *config.NebariConfig, result *nic.DeployResult, dryRun bool) string {
	if cfg.Domain == "" || dryRun {
		return ""
	}
	return serviceURL(cfg.Domain, result.LBEndpoint)
}

// serviceURL is https://<host>, with the endpoint's HTTPS port when it is
// not the standard one.
func serviceURL(host string, lb *endpoint.LoadBalancerEndpoint) string {
	if lb != nil && lb.Port != 0 && lb.Port != 443 {
		return fmt.Sprintf("https://%s:%d", host, lb.Port)
	}
	return "https://" + host
}

// deploySteps returns the numbered steps printed after a deploy, in every
// format: the DNS records or /etc/hosts line to create when no DNS provider
// manages them, the services to open, and how to sign in.
func deploySteps(cfg *config.NebariConfig, result *nic.DeployResult, dryRun bool) []output.Step {
	var steps []output.Step
	if cfg.DNS == nil && cfg.Domain != "" && !dryRun {
		steps = append(steps, dnsStep(cfg, result.LBEndpoint))
	}
	if !result.ArgoCDInstalled {
		return steps
	}
	if cfg.Domain != "" {
		steps = append(steps, openServicesStep(cfg, result))
	} else {
		steps = append(steps, portForwardStep(result))
	}
	return append(steps, output.Step{
		Title: "Sign in as admin",
		Lines: []output.StepLine{{Label: "password", Value: "nic outputs --show-secrets"}},
	})
}

// dnsStep is the records to create by hand. When the endpoint is loopback
// (local kind clusters publish the gateway on host ports of 127.0.0.1) it is
// the /etc/hosts line instead: public DNS records pointing at loopback make
// no sense. The hosts line never carries the port; hosts files map names to
// addresses.
func dnsStep(cfg *config.NebariConfig, lb *endpoint.LoadBalancerEndpoint) output.Step {
	switch {
	case lb != nil && net.ParseIP(lb.IP).IsLoopback():
		return output.Step{Title: "Add this line to /etc/hosts", Required: true, Lines: []output.StepLine{
			{Value: fmt.Sprintf("%s %s keycloak.%s argocd.%s", lb.IP, cfg.Domain, cfg.Domain, cfg.Domain)},
			{Note: "No wildcards: services added later need their hostname on this same line."},
		}}
	case lb != nil:
		recordType, value := "A", lb.IP
		if lb.Hostname != "" {
			recordType, value = "CNAME", lb.Hostname
		}
		var lines []output.StepLine
		for _, name := range []string{cfg.Domain, "*." + cfg.Domain} {
			lines = append(lines,
				output.StepLine{Label: "Type", Value: recordType},
				output.StepLine{Label: "Name", Value: name},
				output.StepLine{Label: "Value", Value: value},
				output.StepLine{},
			)
		}
		lines = append(lines,
			output.StepLine{Note: "Or let NIC manage them: add a dns block with the Cloudflare zone holding " + cfg.Domain + ":"},
			output.StepLine{Value: "dns: {cloudflare: {zone_name: <zone>}}"},
		)
		return output.Step{Title: "Create these DNS records", Required: true, Lines: lines}
	default:
		return output.Step{Title: "Point DNS at the load balancer", Required: true, Lines: []output.StepLine{
			{Note: "The load balancer has no address yet. Wait for it with:"},
			{Value: "nic outputs --wait"},
			{Note: fmt.Sprintf("Then create A (for an IP) or CNAME (for a hostname) records for %s and *.%s pointing at it.", cfg.Domain, cfg.Domain)},
		}}
	}
}

// openServicesStep lists the platform's URLs. Keycloak and the landing page
// are synced by Argo CD after the deploy returns, so they take a while.
func openServicesStep(cfg *config.NebariConfig, result *nic.DeployResult) output.Step {
	var lines []output.StepLine
	if result.KeycloakInstalled {
		lines = append(lines, output.StepLine{Label: "Nebari", Value: serviceURL(cfg.Domain, result.LBEndpoint), URL: true})
	}
	lines = append(lines, output.StepLine{Label: "Argo CD", Value: serviceURL("argocd."+cfg.Domain, result.LBEndpoint), URL: true})
	if result.KeycloakInstalled {
		lines = append(lines,
			output.StepLine{Label: "Keycloak", Value: serviceURL("keycloak."+cfg.Domain, result.LBEndpoint), URL: true, After: "ready in a few minutes"},
			output.StepLine{Note: "Argo CD syncs Keycloak after the deploy; kubectl get pods -n keycloak shows its progress."},
		)
	}
	return output.Step{Title: "Open the services", Lines: lines}
}

// portForwardStep reaches the services without a domain.
func portForwardStep(result *nic.DeployResult) output.Step {
	lines := []output.StepLine{
		{Label: "Argo CD", Value: "kubectl port-forward svc/argocd-server -n argocd 8080:443"},
		{Note: "then open https://localhost:8080"},
	}
	if result.KeycloakInstalled {
		lines = append(lines,
			output.StepLine{Label: "Keycloak", Value: "kubectl port-forward svc/keycloak -n keycloak 8081:80"},
			output.StepLine{Note: "then open http://localhost:8081 (ready in a few minutes)"},
		)
	}
	return output.Step{Title: "Reach the services with port-forwarding", Lines: lines}
}
