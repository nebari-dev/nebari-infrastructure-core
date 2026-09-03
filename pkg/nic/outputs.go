package nic

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/argocd"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/endpoint"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/providers/cluster"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/status"
)

// PlatformOutputs holds the entry points of a deployed Nebari platform: the
// addresses and bootstrap credentials a consumer needs to talk to it.
//
// Field names double as the identifiers reported in unresolved-field errors,
// so they match the JSON keys the CLI prints.
type PlatformOutputs struct {
	Domain                     string `json:"domain"`
	KeycloakIssuerURL          string `json:"keycloak_issuer_url"`
	KeycloakAdminPassword      string `json:"keycloak_admin_password"`
	KeycloakRealmAdminPassword string `json:"keycloak_realm_admin_password"`
	ArgoCDAdminPassword        string `json:"argocd_admin_password"`
	GatewayAddress             string `json:"gateway_address"`
}

// gatewayAddressField is PlatformOutputs.GatewayAddress's JSON key, which is
// also its identifier in unresolved-field errors (see PlatformOutputs). It is
// the one output resolved through a chain of checks rather than a single
// read, so it names its unresolved entry from several places.
const gatewayAddressField = "gateway_address"

// OutputsOptions controls how Outputs handles fields that are not yet
// available. Some outputs materialize after the deploy call returns: the Argo
// CD server writes argocd-initial-admin-secret itself on first start, and the
// gateway address appears once the load balancer is provisioned.
type OutputsOptions struct {
	// Wait polls for unresolved fields instead of failing on the first read.
	Wait bool

	// Timeout bounds the polling window. Zero means endpoint.DefaultTimeout.
	// Ignored unless Wait is set.
	Timeout time.Duration

	// PollInterval is the delay between polls. Zero means
	// endpoint.DefaultPollInterval. Ignored unless Wait is set.
	//
	// Deliberately not exposed as a CLI flag: operators have no reason to tune
	// how often NIC re-reads the cluster, and --timeout is the knob that
	// matters to them. It exists so tests can poll on a millisecond scale.
	PollInterval time.Duration
}

// DefaultOutputsTimeout is the polling window Outputs uses when
// OutputsOptions.Timeout is zero. Exported so a caller can present it as a
// flag default without reaching past pkg/nic for a value pkg/nic owns: if this
// package ever picks a different default, the CLI follows automatically.
const DefaultOutputsTimeout = endpoint.DefaultTimeout

// apiRequestTimeout bounds each individual API request. Without it a request
// to an API server whose connection has been black-holed - dropped packets,
// no RST - blocks until the kernel gives up on the TCP connection, which can
// outlast --timeout by minutes. CI is this command's primary consumer, so an
// unbounded read is a hung job rather than a failed one.
const apiRequestTimeout = 30 * time.Second

// restConfigFromKubeconfig builds a client configuration whose requests are
// individually bounded, so no single read can outlive the caller's deadline.
func restConfigFromKubeconfig(ctx context.Context, kubeconfigBytes []byte) (*rest.Config, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	_, span := tracer.Start(ctx, "nic.restConfigFromKubeconfig")
	defer span.End()

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	restConfig.Timeout = apiRequestTimeout
	span.SetAttributes(attribute.String("request_timeout", apiRequestTimeout.String()))

	return restConfig, nil
}

// unresolved records one output that could not be read, with the reason it
// could not. Reporting the reason is the point of the command: an empty value
// is never returned in place of a missing object, because that is exactly the
// failure mode consumers cannot detect.
type unresolved struct {
	field  string
	reason string

	// permanent marks a field no amount of waiting can resolve, because the
	// cause is the configuration rather than a deployment still converging.
	// --wait fails on these immediately instead of reporting a config mistake
	// as a platform that never came up.
	permanent bool
}

// Outputs returns the entry points of the platform described by cfg. Every
// field either resolves or the call fails naming each unresolved field, so a
// caller can never mistake a renamed secret for an empty password.
func (c *Client) Outputs(ctx context.Context, cfg *config.NebariConfig, opts OutputsOptions) (*PlatformOutputs, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.Outputs")
	defer span.End()

	span.SetAttributes(attribute.Bool("wait", opts.Wait))

	reg := c.registry

	if err := cfg.Validate(validateOptions(ctx, reg)); err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	clusterProvider, err := reg.ClusterProviders.Get(ctx, cfg.Cluster.ProviderName())
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get cluster provider: %w", err)
	}

	kubeconfigBytes, err := clusterProvider.GetKubeconfig(ctx, cfg.ProjectName, cfg.Cluster)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("get kubeconfig: %w", err)
	}

	restConfig, err := restConfigFromKubeconfig(ctx, kubeconfigBytes)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	k8sClient, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	// The same TemplateData the deploy path renders manifests from, so the
	// secret names, keys, and issuer URL formula reported here cannot drift
	// from the objects this binary placed in the cluster.
	data := argocd.NewTemplateData(cfg, nil, clusterProvider.InfraSettings(cfg.Cluster))

	outputs, err := resolveOutputs(ctx, k8sClient, data, opts)
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	return outputs, nil
}

// resolveOutputs reads the platform's entry points from the cluster. It is
// separate from Outputs so it can be exercised against a fake clientset
// without a provider registry or a real cluster.
func resolveOutputs(ctx context.Context, client kubernetes.Interface, data argocd.TemplateData, opts OutputsOptions) (*PlatformOutputs, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.resolveOutputs")
	defer span.End()

	if !opts.Wait {
		outputs, missing := readOutputs(ctx, client, data)
		if len(missing) > 0 {
			err := unresolvedError(missing, false, 0)
			span.RecordError(err)
			return nil, err
		}
		return outputs, nil
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultOutputsTimeout
	}
	pollInterval := opts.PollInterval
	if pollInterval == 0 {
		pollInterval = endpoint.DefaultPollInterval
	}

	span.SetAttributes(
		attribute.String("timeout", timeout.String()),
		attribute.String("poll_interval", pollInterval.String()),
	)

	// Bound the polling window on the context, not just on a timer. A timer is
	// only consulted between polls, so it cannot interrupt a read already in
	// flight; a context can, because client-go cancels the underlying request
	// when it is cancelled. Without this, --timeout is a floor rather than a
	// ceiling whenever the API server is slow to answer.
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	outputs, missing := readOutputs(pollCtx, client, data)
	for len(missing) > 0 {
		if hasPermanent(missing) {
			err := unresolvedError(missing, false, 0)
			span.RecordError(err)
			return nil, err
		}

		status.Progressf(ctx, "Waiting for platform outputs: %s", strings.Join(fieldNames(missing), ", "))

		select {
		case <-pollCtx.Done():
			// pollCtx is done either because the operator interrupted the
			// command or because the polling window expired. Reporting an
			// interrupted run as "the platform never converged" would send the
			// operator looking for a deployment fault that is not there, so
			// consult the parent to tell the two apart.
			if ctx.Err() != nil {
				err := fmt.Errorf("context cancelled while waiting for platform outputs: %w", ctx.Err())
				span.RecordError(err)
				return nil, err
			}
			err := unresolvedError(missing, true, timeout)
			span.RecordError(err)
			return nil, err
		case <-ticker.C:
			outputs, missing = readOutputs(pollCtx, client, data)
		}
	}

	return outputs, nil
}

// readOutputs performs one pass over the cluster, returning what it could read
// and an entry for every field it could not.
func readOutputs(ctx context.Context, client kubernetes.Interface, data argocd.TemplateData) (*PlatformOutputs, []unresolved) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.readOutputs")
	defer span.End()

	outputs := &PlatformOutputs{
		Domain:            data.Domain,
		KeycloakIssuerURL: data.KeycloakIssuerURL,
	}

	var missing []unresolved

	// NewTemplateData leaves the issuer URL empty when no domain is
	// configured. Reporting that as unresolved beats emitting a bare
	// "/realms/nebari" that fails much later, at token validation.
	if outputs.KeycloakIssuerURL == "" {
		missing = append(missing, unresolved{
			field:     "keycloak_issuer_url",
			reason:    "no domain configured; set domain in the configuration file",
			permanent: true,
		})
	}

	secretReads := []struct {
		field     string
		namespace string
		name      string
		key       string
		target    *string
	}{
		{
			field:     "keycloak_admin_password",
			namespace: data.KeycloakAdminSecretNamespace,
			name:      data.KeycloakAdminSecretName,
			key:       data.KeycloakAdminPasswordKey,
			target:    &outputs.KeycloakAdminPassword,
		},
		{
			field:     "keycloak_realm_admin_password",
			namespace: data.KeycloakNamespace,
			name:      data.RealmAdminSecretName,
			key:       data.RealmAdminPasswordKey,
			target:    &outputs.KeycloakRealmAdminPassword,
		},
		{
			field:     "argocd_admin_password",
			namespace: argocd.DefaultNamespace,
			name:      argocd.ArgoCDInitialAdminSecretName,
			key:       argocd.ArgoCDInitialAdminSecretKey,
			target:    &outputs.ArgoCDAdminPassword,
		},
	}

	for _, read := range secretReads {
		// Once the deadline has passed there is no point issuing further
		// reads: each would only wait out its own request timeout, turning one
		// expired deadline into several. Report the field as unread instead.
		if err := ctx.Err(); err != nil {
			missing = append(missing, unresolved{field: read.field, reason: abandonedReason(err)})
			continue
		}

		value, err := secretValue(ctx, client, read.namespace, read.name, read.key)
		if err != nil {
			missing = append(missing, unresolved{field: read.field, reason: err.Error()})
			continue
		}
		*read.target = value
	}

	// The gateway address is read last, so the deadline may already have
	// passed by the time its turn comes.
	if data.GatewayHostAddress != "" {
		// A host-port gateway (local kind clusters) has no load balancer
		// status to read: the address comes from the provider, so it is
		// substantiated before it is reported. The Envoy service must carry
		// the pinned NodePorts the kind host ports point at, and the gateway
		// must answer a real HTTPS request at the address. Either failing
		// reports the field unresolved instead of an address nothing serves.
		if err := ctx.Err(); err != nil {
			missing = append(missing, unresolved{field: gatewayAddressField, reason: abandonedReason(err)})
		} else if err := endpoint.CheckNodePorts(ctx, client, []int32{cluster.GatewayHTTPNodePort, cluster.GatewayHTTPSNodePort}); err != nil {
			missing = append(missing, unresolved{field: gatewayAddressField, reason: err.Error()})
		} else if err := endpoint.ProbeGateway(ctx, data.GatewayHostAddress, data.Domain, data.HTTPSPort); err != nil {
			missing = append(missing, unresolved{field: gatewayAddressField, reason: err.Error()})
		} else {
			outputs.GatewayAddress = data.GatewayHostAddress
		}
	} else if err := ctx.Err(); err != nil {
		missing = append(missing, unresolved{field: gatewayAddressField, reason: abandonedReason(err)})
	} else if ep, err := endpoint.Check(ctx, client); err != nil {
		missing = append(missing, unresolved{field: gatewayAddressField, reason: err.Error()})
	} else {
		// Prefer the hostname: cloud load balancers publish a hostname and
		// leave the IP empty, and the hostname is what DNS should point at.
		outputs.GatewayAddress = ep.Hostname
		if outputs.GatewayAddress == "" {
			outputs.GatewayAddress = ep.IP
		}
		if outputs.GatewayAddress == "" {
			missing = append(missing, unresolved{
				field:  gatewayAddressField,
				reason: "load balancer ingress has neither a hostname nor an IP",
			})
		}
	}

	span.SetAttributes(
		attribute.Int("unresolved.count", len(missing)),
		attribute.StringSlice("unresolved.fields", fieldNames(missing)),
	)

	return outputs, missing
}

// secretValue reads one key from one secret, distinguishing "no such secret"
// from "secret exists but has no such key". The second case is the one a
// kubectl/jsonpath pipeline reports as success with an empty value.
func secretValue(ctx context.Context, client kubernetes.Interface, namespace, name, key string) (string, error) {
	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "nic.secretValue")
	defer span.End()

	// Coordinates only: the value read here is a credential and must never
	// reach a span attribute.
	span.SetAttributes(
		attribute.String("secret.namespace", namespace),
		attribute.String("secret.name", name),
		attribute.String("secret.key", key),
	)

	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("secret %s/%s not found", namespace, name)
		}
		return "", fmt.Errorf("read secret %s/%s: %w", namespace, name, err)
	}

	value, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s/%s has no key %q (keys present: %s)",
			namespace, name, key, strings.Join(secretKeys(secret.Data), ", "))
	}
	if len(value) == 0 {
		return "", fmt.Errorf("secret %s/%s has an empty value for key %q", namespace, name, key)
	}

	return string(value), nil
}

// abandonedReason describes a field left unread because the deadline passed
// before its turn came, distinguishing it from a field whose object is
// genuinely absent.
func abandonedReason(err error) string {
	return fmt.Sprintf("not read: %v", err)
}

func secretKeys(data map[string][]byte) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func hasPermanent(missing []unresolved) bool {
	for _, m := range missing {
		if m.permanent {
			return true
		}
	}
	return false
}

func fieldNames(missing []unresolved) []string {
	names := make([]string, 0, len(missing))
	for _, m := range missing {
		names = append(names, m.field)
	}
	return names
}

// unresolvedError renders every unresolved field with its reason in one error,
// so a single run tells the operator everything that is wrong rather than one
// thing per retry.
func unresolvedError(missing []unresolved, waited bool, timeout time.Duration) error {
	details := make([]string, 0, len(missing))
	for _, m := range missing {
		details = append(details, fmt.Sprintf("%s (%s)", m.field, m.reason))
	}

	if waited {
		return fmt.Errorf("timed out after %s waiting for platform outputs: %s",
			timeout, strings.Join(details, "; "))
	}

	// Suggesting --wait for a configuration error would send the operator off
	// to poll a condition that can never change.
	if hasPermanent(missing) {
		return fmt.Errorf("unresolved platform outputs: %s", strings.Join(details, "; "))
	}

	return fmt.Errorf("unresolved platform outputs: %s (pass --wait if the deployment is still converging)",
		strings.Join(details, "; "))
}
