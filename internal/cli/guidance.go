package cli

import (
	"fmt"
	"net"

	"github.com/nebari-dev/nebari-infrastructure-core/internal/output"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

// deployGuidance returns the sections printed after a deploy: how to reach
// Argo CD and Keycloak, and the DNS records (or /etc/hosts line) to create
// when no DNS provider manages them. Each format renders the same sections,
// so the guidance can't drift between them.
func deployGuidance(cfg *config.NebariConfig, result *nic.DeployResult, dryRun bool) []output.Section {
	var sections []output.Section
	if result.ArgoCDInstalled {
		sections = append(sections, argoCDSection(cfg))
	}
	if result.KeycloakInstalled {
		sections = append(sections, keycloakSection(cfg))
	}
	if cfg.DNS == nil && cfg.Domain != "" && !dryRun {
		sections = append(sections, dnsSection(cfg, result.LBEndpoint))
	}
	return sections
}

// dnsSection explains the DNS records to create by hand. When the endpoint is
// loopback (local kind clusters publish the gateway on host ports of
// 127.0.0.1) it explains /etc/hosts instead: public DNS records pointing at
// loopback make no sense, and /etc/hosts is the whole setup.
func dnsSection(cfg *config.NebariConfig, lb *endpoint.LoadBalancerEndpoint) output.Section {
	if lb != nil && net.ParseIP(lb.IP).IsLoopback() {
		return hostsFileSection(cfg, lb)
	}

	body := []string{
		"",
		"No DNS provider is configured. To access your services, you must manually",
		"configure the following DNS records with your DNS provider:",
		"",
		"Domain: " + cfg.Domain,
		"",
	}
	if lb != nil {
		recordType, value := "A", lb.IP
		if lb.Hostname != "" {
			recordType, value = "CNAME", lb.Hostname
		}
		// One record per block rather than a table: a load balancer hostname
		// is wider than any fixed column, and these values get copied.
		body = append(body, "Required DNS Records:")
		for _, name := range []string{cfg.Domain, "*." + cfg.Domain} {
			body = append(body,
				"",
				"  Type:  "+recordType,
				"  Name:  "+name,
				"  Value: "+value,
			)
		}
	} else {
		body = append(body,
			"The load balancer endpoint is not yet available.",
			"Run the following command to check when it's ready:",
			"",
			"  nic status -f <config-file>",
			"",
			"Once the endpoint is available, create A (for IP) or CNAME (for hostname)",
			fmt.Sprintf("records pointing %s and *.%s to the endpoint.", cfg.Domain, cfg.Domain),
		)
	}
	body = append(body,
		"",
		"To automate DNS management, add a dns block to your configuration:",
		"",
		"  dns:",
		"    cloudflare:",
		"      zone_name: example.com",
		"",
	)
	return output.Section{Title: "DNS CONFIGURATION REQUIRED", Body: body}
}

// hostsFileSection explains the /etc/hosts line for clusters whose gateway is
// published on loopback host ports. The URL carries the HTTPS port when it is
// non-standard; the /etc/hosts line never does (hosts files map names to
// addresses, not ports).
func hostsFileSection(cfg *config.NebariConfig, lb *endpoint.LoadBalancerEndpoint) output.Section {
	url := "https://" + cfg.Domain
	if lb.Port != 0 && lb.Port != 443 {
		url = fmt.Sprintf("https://%s:%d", cfg.Domain, lb.Port)
	}
	return output.Section{Title: "HOSTS FILE CONFIGURATION REQUIRED", Body: []string{
		"",
		fmt.Sprintf("The platform is published on host ports of %s.", lb.IP),
		"Point the platform hostnames at it by adding this line to /etc/hosts:",
		"",
		fmt.Sprintf("  %s %s keycloak.%s argocd.%s", lb.IP, cfg.Domain, cfg.Domain, cfg.Domain),
		"",
		fmt.Sprintf("Then open %s in your browser.", url),
		"",
		"/etc/hosts has no wildcard support: services exposed later on other",
		"subdomains need their hostname appended to the same line.",
		"",
	}}
}

// argoCDSection explains how to reach Argo CD.
func argoCDSection(cfg *config.NebariConfig) output.Section {
	body := []string{
		"",
		"Argo CD has been successfully installed on your cluster.",
		"",
		"To access Argo CD:",
		"",
	}
	if cfg.Domain != "" {
		body = append(body,
			fmt.Sprintf("  UI: https://argocd.%s (after DNS configuration)", cfg.Domain),
			"",
			"Or use port-forwarding:",
			"",
		)
	}
	body = append(body,
		"  kubectl port-forward svc/argocd-server -n argocd 8080:443",
		"  Then visit: https://localhost:8080",
	)
	body = append(body, credentialsLines()...)
	body = append(body, "")
	return output.Section{Title: "ARGO CD INSTALLED", Body: body}
}

// keycloakSection explains how to reach Keycloak once Argo CD has synced it.
func keycloakSection(cfg *config.NebariConfig) output.Section {
	body := []string{
		"",
		"Keycloak has been configured for installation via Argo CD.",
		"It may take several minutes for Keycloak to be fully deployed and ready.",
		"",
		"Check deployment status:",
		"  kubectl get pods -n keycloak",
		"",
		"To access Keycloak after deployment:",
		"",
	}
	if cfg.Domain != "" {
		body = append(body,
			fmt.Sprintf("  UI: https://keycloak.%s (after DNS configuration)", cfg.Domain),
			"",
			"Or use port-forwarding:",
			"",
		)
	}
	body = append(body,
		"  kubectl port-forward svc/keycloak -n keycloak 8080:80",
		"  Then visit: http://localhost:8080",
	)
	body = append(body, credentialsLines()...)
	body = append(body, "", "")
	return output.Section{Title: "KEYCLOAK INSTALLED", Body: body}
}

// credentialsLines is the admin-credentials recipe shared by the Argo CD and
// Keycloak sections.
func credentialsLines() []string {
	return []string{
		"",
		"Get the admin password:",
		"",
		"  nic outputs --show-secrets",
		"",
		"Login credentials:",
		"  Username: admin",
		"  Password: <from command above>",
	}
}

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

// deploySteps returns the numbered steps the pretty and plain formats print
// after a deploy: the DNS records or /etc/hosts line to create when no DNS
// provider manages them, the services to open, and how to sign in. They
// carry the same guidance as deployGuidance's banners.
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

// dnsStep is the records to create by hand, or the /etc/hosts line when the
// gateway is published on loopback (see dnsSection).
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
		lines = append(lines, output.StepLine{Note: "To manage these records automatically, add a dns block to the config."})
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
		lines = append(lines, output.StepLine{Label: "Keycloak", Value: serviceURL("keycloak."+cfg.Domain, result.LBEndpoint), URL: true, After: "ready in a few minutes"})
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
