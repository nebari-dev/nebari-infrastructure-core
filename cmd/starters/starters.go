package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

	"github.com/nebari-dev/nebari-infrastructure-core/pkg/tofu"
)

// placeholder is the sentinel a starter ships instead of an identity-bearing
// value. nic's validation rejects any config still containing it, so an
// unedited starter cannot be deployed by accident; see
// docs/operations/config-placeholders.md.
const placeholder = "CHANGEME"

// provider describes one starter: which config values the reader must supply,
// and any conda dependency the provider needs beyond nic itself.
type provider struct {
	// fields are YAML paths, not line prefixes. A path either resolves to a
	// value or it does not, so a key that moves, gets renamed, or gains a
	// same-named sibling at another level is a hard error naming the path
	// rather than something a text match can silently get wrong.
	fields []string
	deps   string
}

// providers is the set in scope. Extend deliberately: a new entry needs its
// own placeholder paths, and generate refuses to emit a starter that declares
// none rather than shipping one with every real value intact.
var providers = map[string]provider{
	"local": {
		// kind runs everything locally: the certificate is self-signed and the
		// GitOps repo is created for the user, so only the name is theirs.
		fields: []string{"$.project_name"},
		// The local provider drives kind through an embedded Go library, so
		// there is no OpenTofu in this workspace.
		deps: "",
	},
	"aws": {
		fields: []string{
			"$.project_name",
			"$.domain",
			"$.certificate.acme.email",
			"$.repository.existing.url",
			"$.repository.existing.path",
		},
		// Pinning OpenTofu is the point of a pinned toolchain: without it nic
		// downloads an unpinned tofu at deploy time.
		deps: opentofuConstraint(),
	},
}

// opentofuConstraint renders the conda dependency for OpenTofu from the range
// nic itself enforces, rather than restating it.
//
// The two have to agree in both directions. A floor below pkg/tofu.MinVersion
// lets pixi resolve a binary that compatibleVersion then rejects, so nic
// downloads its own copy anyway - silently, and the pin the workspace exists to
// provide is gone. A ceiling above MaxVersionExclusive fails the same way at
// the other end. Deriving it means a bump to either constant reaches the
// starters in the same commit instead of leaving a stale literal behind.
func opentofuConstraint() string {
	return fmt.Sprintf(`opentofu = ">=%s,<%s"`, tofu.MinVersion, tofu.MaxVersionExclusive)
}

// providerNames returns the providers in scope, sorted, so output ordering is
// deterministic regardless of map iteration order.
func providerNames() []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// placeholderConfig rewrites src so that every path in fields carries the
// placeholder instead of its real value, and returns the result.
//
// The edit is done on the source text rather than by marshalling the parsed
// document back out: a round trip would reformat the file and drop the inline
// comments that make the examples worth shipping. Each value's token gives the
// line and the column where the value starts, so replacing from that column to
// the end of the line touches nothing else, and a trailing comment on the same
// line is reattached - those lines are exactly the ones whose hint the reader
// needs most.
func placeholderConfig(src []byte, fields []string) ([]byte, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("no placeholder fields declared")
	}

	file, err := parser.ParseBytes(src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Trailing newline handling: strings.Split on a file ending in "\n" yields
	// a final empty element, which Join restores, so the byte count is stable.
	lines := strings.Split(string(src), "\n")

	for _, field := range fields {
		path, err := yaml.PathString(field)
		if err != nil {
			return nil, fmt.Errorf("%s is not a valid YAML path: %w", field, err)
		}

		node, err := path.FilterFile(file)
		if err != nil {
			return nil, fmt.Errorf("%s resolved to nothing; the example has probably been restructured: %w", field, err)
		}

		tok := node.GetToken()
		if tok == nil {
			return nil, fmt.Errorf("%s has no source position", field)
		}
		// A block scalar's token is the |/> indicator, not the body, so the
		// value spans lines the edit below would leave orphaned. Reject it by
		// type: checking the token text for a newline never fires here.
		if tok.Type == token.LiteralType || tok.Type == token.FoldedType || strings.Contains(tok.Value, "\n") {
			return nil, fmt.Errorf("%s is a multi-line value; only single-line scalars can be placeholdered in place", field)
		}

		lineNo, col := tok.Position.Line, tok.Position.Column
		if lineNo < 1 || lineNo > len(lines) {
			return nil, fmt.Errorf("%s reports line %d, outside the file", field, lineNo)
		}
		line := lines[lineNo-1]
		if col < 1 || col > len(line)+1 {
			return nil, fmt.Errorf("%s reports column %d, outside line %d", field, col, lineNo)
		}

		rebuilt := line[:col-1] + placeholder
		if c := node.GetComment(); c != nil {
			if text := strings.TrimSpace(c.String()); text != "" {
				rebuilt += "  " + text
			}
		}
		lines[lineNo-1] = rebuilt
	}

	out := strings.Join(lines, "\n")

	// Re-parse rather than trust the edit. A starter that no longer loads
	// would still be "rejected" by nic validate, just for the wrong reason,
	// and that failure is easy to mistake for the placeholder gate working.
	if _, err := parser.ParseBytes([]byte(out), parser.ParseComments); err != nil {
		return nil, fmt.Errorf("placeholdered config no longer parses: %w", err)
	}
	return []byte(out), nil
}

// renderPixi substitutes the workspace template's tokens. Kept as plain text
// replacement because the template is a fixed file in this repo with three
// tokens, not user input.
func renderPixi(tmpl []byte, name, nicVersion, deps string) ([]byte, error) {
	out := string(tmpl)
	for token, value := range map[string]string{
		"__PROVIDER__":      name,
		"__NIC_VERSION__":   nicVersion,
		"__PROVIDER_DEPS__": deps,
	} {
		out = strings.ReplaceAll(out, token, value)
	}
	if i := strings.Index(out, "__"); i != -1 {
		return nil, fmt.Errorf("unsubstituted template token near %q", out[i:min(i+40, len(out))])
	}
	return []byte(out), nil
}
