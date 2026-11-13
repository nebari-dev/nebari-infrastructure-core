package local

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// The platform domain resolves to 127.0.0.1 on the host (through /etc/hosts or
// a loopback domain such as lvh.me), which is where kind publishes the gateway.
// Inside the cluster that address is the pod itself, so pods calling the
// platform's own hostnames (Keycloak, other apps) reach nothing. A rewrite in
// the cluster's CoreDNS answers those names with the gateway service instead,
// so the same URLs work from the browser and from pods (#613).
const (
	corednsNamespace = metav1.NamespaceSystem
	corednsConfigMap = "coredns"
	corefileKey      = "Corefile"

	platformDNSBegin = "# BEGIN nebari platform domain (managed by nic)"
	platformDNSEnd   = "# END nebari platform domain"
)

// kubernetesPlugin matches the kubernetes plugin line in a Corefile server
// block. The rewrite goes right before it, so it applies before the cluster's
// own names are resolved.
var kubernetesPlugin = regexp.MustCompile(`(?m)^([ \t]*)kubernetes\s`)

// dnsName accepts plain hostnames only. The domain is written into the
// Corefile, so anything else (spaces, braces, newlines) could change its
// structure.
var dnsName = regexp.MustCompile(`^[A-Za-z0-9]([-A-Za-z0-9.]*[A-Za-z0-9])?$`)

// ensurePlatformDNS points the platform domain and all its subdomains at the
// gateway service in the cluster's CoreDNS config. It only writes when the
// config changes, so repeated deploys leave it alone. An empty domain removes
// the rule. CoreDNS's reload plugin picks the change up without a restart.
func ensurePlatformDNS(ctx context.Context, client kubernetes.Interface, domain string) error {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "local.ensurePlatformDNS")
	defer span.End()

	span.SetAttributes(attribute.String("domain", domain))

	changed := false
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := client.CoreV1().ConfigMaps(corednsNamespace).Get(ctx, corednsConfigMap, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("read CoreDNS config %s/%s: %w", corednsNamespace, corednsConfigMap, err)
		}
		current, ok := cm.Data[corefileKey]
		if !ok {
			return fmt.Errorf("CoreDNS config %s/%s has no %s key", corednsNamespace, corednsConfigMap, corefileKey)
		}
		updated, err := withPlatformDNS(current, domain)
		if err != nil {
			return err
		}
		if updated == current {
			return nil
		}
		cm.Data[corefileKey] = updated
		if _, err := client.CoreV1().ConfigMaps(corednsNamespace).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("configure in-cluster DNS for %q: %w", domain, err)
	}

	span.SetAttributes(attribute.Bool("changed", changed))
	if changed && domain != "" {
		status.Send(ctx, status.NewUpdate(status.LevelInfo, fmt.Sprintf("Pointed %s and its subdomains at the gateway inside the cluster", domain)).
			WithResource("provider").
			WithAction("deploy").
			WithMetadata("domain", domain))
	}
	return nil
}

// withPlatformDNS returns corefile with the platform domain rewrite placed
// before the kubernetes plugin, replacing any rule an earlier deploy wrote. An
// empty domain removes the rule.
func withPlatformDNS(corefile, domain string) (string, error) {
	stripped, err := withoutPlatformDNS(corefile)
	if err != nil {
		return "", err
	}
	if domain == "" {
		return stripped, nil
	}
	if !dnsName.MatchString(domain) {
		return "", fmt.Errorf("domain %q is not a valid DNS name", domain)
	}

	loc := kubernetesPlugin.FindStringSubmatchIndex(stripped)
	if loc == nil {
		return "", fmt.Errorf("CoreDNS config has no kubernetes plugin to place the platform domain rule before")
	}
	indent := stripped[loc[2]:loc[3]]

	// Anchored, so names that merely contain the domain (notnebari.example.com,
	// nebari.example.com.evil.org) keep resolving normally. Query names are
	// fully qualified, hence the trailing dot. answer auto rewrites the
	// response back to the name the pod asked for.
	rule := fmt.Sprintf(`rewrite name regex ^(.*\.)?%s\.$ %s.%s.svc.cluster.local. answer auto`,
		regexp.QuoteMeta(strings.ToLower(domain)), cluster.GatewayServiceName, cluster.GatewayServiceNamespace)

	block := indent + platformDNSBegin + "\n" +
		indent + rule + "\n" +
		indent + platformDNSEnd + "\n"
	return stripped[:loc[0]] + block + stripped[loc[0]:], nil
}

// withoutPlatformDNS removes the managed block, if present.
func withoutPlatformDNS(corefile string) (string, error) {
	lines := strings.SplitAfter(corefile, "\n")
	out := make([]string, 0, len(lines))
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == platformDNSBegin:
			if inBlock {
				return "", fmt.Errorf("CoreDNS config has a nested %q marker", platformDNSBegin)
			}
			inBlock = true
		case trimmed == platformDNSEnd:
			if !inBlock {
				return "", fmt.Errorf("CoreDNS config has %q without %q", platformDNSEnd, platformDNSBegin)
			}
			inBlock = false
		case !inBlock:
			out = append(out, line)
		}
	}
	if inBlock {
		return "", fmt.Errorf("CoreDNS config has %q without %q", platformDNSBegin, platformDNSEnd)
	}
	return strings.Join(out, ""), nil
}
