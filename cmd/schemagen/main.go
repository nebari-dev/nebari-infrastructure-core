// schemagen emits JSON Schema + commented YAML reference documents for
// nebari-config.yaml and each registered provider's Config struct. It is
// an internal build/CI tool, not a user-facing subcommand of nic.
//
// Currently a skeleton: it enumerates the registered providers via
// pkg/nic.RegisteredConfigTypes and reports what it would generate, but
// the underlying configschema.Generate is not yet implemented. The
// actual generation + file writing lands in a follow-up commit on the
// same branch.
//
// Intended invocation (once complete):
//
//	go run ./cmd/schemagen -out ./schemas
//
// Flags:
//
//	-out         output directory (default "./schemas")
//	-providers   comma-separated subset to regenerate (default: all registered)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/nic"
)

func main() {
	var (
		outDir    string
		providers string
	)
	flag.StringVar(&outDir, "out", "./schemas", "output directory for generated schema files")
	flag.StringVar(&providers, "providers", "", "comma-separated subset of providers to regenerate (default: all registered)")
	flag.Parse()

	ctx := context.Background()

	client, err := nic.NewClient(ctx)
	if err != nil {
		slog.Error("build nic client", "error", err)
		os.Exit(1)
	}

	types := client.RegisteredConfigTypes(ctx)

	cluster := sortedKeys(types.Cluster)
	dns := sortedKeys(types.DNS)

	filter := parseFilter(providers)
	if len(filter) > 0 {
		cluster = filterNames(cluster, filter)
		dns = filterNames(dns, filter)
	}

	fmt.Printf("schemagen — output directory: %s\n", outDir)
	fmt.Printf("cluster providers (%d):\n", len(cluster))
	for _, name := range cluster {
		fmt.Printf("  %-10s → %s\n", name, types.Cluster[name].String())
	}
	fmt.Printf("dns providers (%d):\n", len(dns))
	for _, name := range dns {
		fmt.Printf("  %-10s → %s\n", name, types.DNS[name].String())
	}
	fmt.Println()
	fmt.Println("schemagen is a skeleton. configschema.Generate is not yet")
	fmt.Println("implemented; no schema files were written. See the follow-up")
	fmt.Println("commit on the feat/config-schema-gen branch.")
}

// sortedKeys returns the keys of m in deterministic order. The schema
// output must be reproducible for the CI drift gate to work.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parseFilter(raw string) map[string]struct{} {
	if raw == "" {
		return nil
	}
	out := make(map[string]struct{})
	for name := range strings.SplitSeq(raw, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

func filterNames(all []string, want map[string]struct{}) []string {
	out := make([]string, 0, len(all))
	for _, name := range all {
		if _, ok := want[name]; ok {
			out = append(out, name)
		}
	}
	return out
}
