package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/config"
	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

const (
	formatTable = "table"
	formatJSON  = "json"

	redactedPlaceholder = "<redacted: use --show-secrets>"
)

var (
	outputsConfigFile  string
	outputsFormat      string
	outputsShowSecrets bool
	outputsWait        bool
	outputsTimeout     time.Duration

	outputsCmd = &cobra.Command{
		Use:   "outputs",
		Short: "Print the deployed platform's entry points",
		Long: `Print the entry points of a deployed Nebari platform: its domain, the
Keycloak issuer URL, the gateway address, and the bootstrap admin passwords for
Keycloak and Argo CD.

Because the same binary renders the manifests that place these objects, the
reported names and formulas always match the platform it deployed. Consumers
should call this command rather than reading secrets out of the cluster
themselves, which goes stale silently when a release moves an object.

Every field either resolves or the command exits non-zero naming each field it
could not read. Some fields materialize after a deploy returns (the Argo CD
server writes its own initial admin secret; the gateway address waits on the
load balancer) - use --wait to poll for those.

Secret values are redacted unless --show-secrets is passed. Under --format json
a redacted field is null and is listed under a "redacted" key, so a caller that
forgets the flag gets null rather than a placeholder it might use as a password.
Status messages go to stderr, so --format json is safe to pipe.`,
		Example: `  # Human-readable summary, secrets redacted
  nic outputs

  # Machine-readable, for scripts and CI
  nic outputs --format json --show-secrets

  # Immediately after a deploy, while the platform is still converging
  nic outputs --format json --show-secrets --wait --timeout 10m`,
		RunE: runOutputs,
	}
)

func init() {
	outputsCmd.Flags().StringVarP(&outputsConfigFile, "file", "f", "", "Path to nebari-config.yaml file (auto-discovered if omitted)")
	outputsCmd.Flags().StringVar(&outputsFormat, "format", formatTable, "Output format: table or json")
	outputsCmd.Flags().BoolVar(&outputsShowSecrets, "show-secrets", false, "Print secret values instead of redacting them")
	outputsCmd.Flags().BoolVar(&outputsWait, "wait", false, "Poll for outputs that are not yet available")
	outputsCmd.Flags().DurationVar(&outputsTimeout, "timeout", nic.DefaultOutputsTimeout, "How long to poll when --wait is set")
}

// validateOutputsFlags rejects flag combinations before any cluster work
// begins. Both cases below are operator typos, and both are otherwise reported
// only after the command has spent its whole polling window - a five-minute
// wait that ends in "unsupported output format" is a poor trade.
func validateOutputsFlags(format string, wait, timeoutChanged bool) error {
	switch format {
	case formatTable, formatJSON:
	default:
		return fmt.Errorf("unsupported output format %q: expected %q or %q", format, formatTable, formatJSON)
	}

	if timeoutChanged && !wait {
		return fmt.Errorf("--timeout has no effect without --wait: it bounds the polling window, and without --wait the command performs a single read")
	}

	return nil
}

func runOutputs(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	if err := validateOutputsFlags(outputsFormat, outputsWait, cmd.Flags().Changed("timeout")); err != nil {
		return err
	}

	configFile, err := resolveConfigFile(outputsConfigFile)
	if err != nil {
		return err
	}

	tracer := otel.Tracer("nebari-infrastructure-core")
	ctx, span := tracer.Start(ctx, "cmd.outputs")
	defer span.End()

	span.SetAttributes(
		attribute.String("config.file", configFile),
		attribute.String("format", outputsFormat),
		attribute.Bool("show_secrets", outputsShowSecrets),
		attribute.Bool("wait", outputsWait),
	)

	cfg, err := config.ParseConfig(ctx, configFile)
	if err != nil {
		span.RecordError(err)
		return err
	}

	client, err := nic.NewClient(ctx)
	if err != nil {
		span.RecordError(err)
		return err
	}

	// Status updates go to stderr so --format json stays pipeable.
	ctx, cleanup := nic.StartSlogHandler(ctx, slog.Default())
	defer cleanup()

	// Timeout is passed unconditionally but bounds only the polling window, so
	// it has no effect without --wait.
	outputs, err := client.Outputs(ctx, cfg, nic.OutputsOptions{
		Wait:    outputsWait,
		Timeout: outputsTimeout,
	})
	if err != nil {
		span.RecordError(err)
		return err
	}

	rendered, err := formatOutputs(outputs, outputsFormat, outputsShowSecrets)
	if err != nil {
		span.RecordError(err)
		return err
	}

	if _, err := os.Stdout.Write(rendered); err != nil {
		span.RecordError(err)
		return fmt.Errorf("write outputs to stdout: %w", err)
	}

	return nil
}

// outputField describes one platform output. The table renderer and the
// redacted list are both derived from this single declaration, so a field
// cannot be present in one and missing from the other - the drift that made
// the downstream kubectl recipes this command replaces go stale silently.
// Adding a field (#447's gateway trust anchor is the anticipated next one)
// means one entry here plus its slot in outputsJSON.
type outputField struct {
	// key matches the JSON tag on nic.PlatformOutputs and the identifier the
	// resolver reports for an unresolved field.
	key   string
	label string

	// secret marks a field redacted unless --show-secrets is passed.
	secret bool

	value func(*nic.PlatformOutputs) string
}

var outputFields = []outputField{
	{
		key:   "domain",
		label: "Domain",
		value: func(o *nic.PlatformOutputs) string { return o.Domain },
	},
	{
		key:   "keycloak_issuer_url",
		label: "Keycloak issuer URL",
		value: func(o *nic.PlatformOutputs) string { return o.KeycloakIssuerURL },
	},
	{
		key:   "gateway_address",
		label: "Gateway address",
		value: func(o *nic.PlatformOutputs) string { return o.GatewayAddress },
	},
	{
		key:    "keycloak_admin_password",
		label:  "Keycloak admin password",
		secret: true,
		value:  func(o *nic.PlatformOutputs) string { return o.KeycloakAdminPassword },
	},
	{
		key:    "keycloak_realm_admin_password",
		label:  "Keycloak realm admin password",
		secret: true,
		value:  func(o *nic.PlatformOutputs) string { return o.KeycloakRealmAdminPassword },
	},
	{
		key:    "argocd_admin_password",
		label:  "Argo CD admin password",
		secret: true,
		value:  func(o *nic.PlatformOutputs) string { return o.ArgoCDAdminPassword },
	},
}

// redactedFieldKeys lists the fields a run without --show-secrets withholds,
// sorted so a consumer can diff the list across runs.
func redactedFieldKeys() []string {
	keys := make([]string, 0, len(outputFields))
	for _, field := range outputFields {
		if field.secret {
			keys = append(keys, field.key)
		}
	}
	sort.Strings(keys)
	return keys
}

// outputsJSON mirrors nic.PlatformOutputs with pointers for the secret fields,
// so a redacted run renders them as null rather than as a placeholder string a
// consumer might use as a password.
type outputsJSON struct {
	Domain                     string   `json:"domain"`
	KeycloakIssuerURL          string   `json:"keycloak_issuer_url"`
	KeycloakAdminPassword      *string  `json:"keycloak_admin_password"`
	KeycloakRealmAdminPassword *string  `json:"keycloak_realm_admin_password"`
	ArgoCDAdminPassword        *string  `json:"argocd_admin_password"`
	GatewayAddress             string   `json:"gateway_address"`
	Redacted                   []string `json:"redacted,omitempty"`
}

// formatOutputs renders outputs in the requested format. showSecrets governs
// whether the three password fields carry their real values.
func formatOutputs(outputs *nic.PlatformOutputs, format string, showSecrets bool) ([]byte, error) {
	switch format {
	case formatJSON:
		return marshalOutputsJSON(outputs, showSecrets)
	case formatTable:
		return renderOutputsTable(outputs, showSecrets), nil
	default:
		return nil, fmt.Errorf("unsupported output format %q: expected %q or %q", format, formatTable, formatJSON)
	}
}

func marshalOutputsJSON(outputs *nic.PlatformOutputs, showSecrets bool) ([]byte, error) {
	payload := outputsJSON{
		Domain:            outputs.Domain,
		KeycloakIssuerURL: outputs.KeycloakIssuerURL,
		GatewayAddress:    outputs.GatewayAddress,
	}

	if showSecrets {
		payload.KeycloakAdminPassword = &outputs.KeycloakAdminPassword
		payload.KeycloakRealmAdminPassword = &outputs.KeycloakRealmAdminPassword
		payload.ArgoCDAdminPassword = &outputs.ArgoCDAdminPassword
	} else {
		payload.Redacted = redactedFieldKeys()
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal outputs: %w", err)
	}

	return append(encoded, '\n'), nil
}

func renderOutputsTable(outputs *nic.PlatformOutputs, showSecrets bool) []byte {
	secret := func(value string) string {
		if showSecrets {
			return value
		}
		return redactedPlaceholder
	}

	width := 0
	for _, field := range outputFields {
		if len(field.label) > width {
			width = len(field.label)
		}
	}

	var b strings.Builder
	for _, field := range outputFields {
		value := field.value(outputs)
		if field.secret {
			value = secret(value)
		}
		fmt.Fprintf(&b, "%-*s  %s\n", width, field.label, value)
	}

	return []byte(b.String())
}
