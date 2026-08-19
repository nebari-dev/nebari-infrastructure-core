package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// PlaceholderValue is the literal sentinel that marks an unfilled config value.
// Any scalar value or mapping key whose text contains this token (case-sensitive)
// is treated as a placeholder and rejected before any provider API call.
//
// A literal sentinel is used, rather than pattern-matching values like
// "example.com", so the check is explicit and greppable and never rejects a
// user who legitimately owns such a value. Example and starter configs must use
// this exact token wherever the reader is expected to substitute their own
// value.
const PlaceholderValue = "CHANGEME"

// PlaceholderError reports that a config still holds the CHANGEME placeholder.
// FieldPaths lists every dotted path (built from mapping keys and sequence
// indices) whose scalar value or key contains the sentinel, e.g.
// "cluster.aws.region" or "cluster.aws.node_groups.CHANGEME". Callers that know
// the config file path (the cmd layer) wrap this so the user sees both the
// fields and the file.
type PlaceholderError struct {
	// FieldPaths is the sorted set of offending paths. It always has at least
	// one entry.
	FieldPaths []string
}

func (e *PlaceholderError) Error() string {
	quoted := make([]string, len(e.FieldPaths))
	for i, p := range e.FieldPaths {
		quoted[i] = fmt.Sprintf("%q", p)
	}
	field := "field"
	if len(quoted) > 1 {
		field = "fields"
	}
	return fmt.Sprintf("placeholder value %q found in %s %s; edit the config before deploying",
		PlaceholderValue, field, strings.Join(quoted, ", "))
}

// CheckPlaceholders scans the raw YAML config for the CHANGEME sentinel and
// returns a *PlaceholderError naming every offending field, or nil if none are
// found.
//
// It walks the parsed YAML node tree (not the Go struct) so the check is
// independent of NebariConfig's fields: it covers provider blocks, nested maps,
// sequences, and — unlike a struct walk — mapping KEYS. Only scalar values
// (including the contents of "|" and ">" block scalars) and mapping keys are
// inspected; comments are never scanned, so a CHANGEME inside a YAML comment
// does not trip the check.
//
// The check is intended for the validate and deploy paths only. It is not part
// of NebariConfig.Validate, so destroy/kubeconfig (which only need a parseable
// config) are not gated on it.
//
// A malformed document is reported as a parse error rather than as a silent
// "no placeholders" result, so a caller that passes unparsed bytes cannot read
// garbage as clean. At the call sites in cmd/nic this cannot fire: they run
// CheckPlaceholders only after ParseConfig has parsed the same file with the
// same library. Note that the file is re-read between those two steps, so the
// scanned bytes are not guaranteed to be byte-identical to the parsed ones.
func CheckPlaceholders(raw []byte) error {
	file, err := parser.ParseBytes(raw, 0)
	if err != nil {
		return fmt.Errorf("cannot scan config for placeholders: %w", err)
	}

	var found []string
	for _, doc := range file.Docs {
		scanPlaceholderNode(doc.Body, "", &found)
	}
	if len(found) == 0 {
		return nil
	}

	slices.Sort(found)
	found = slices.Compact(found)
	return &PlaceholderError{FieldPaths: found}
}

// scanPlaceholderNode recursively walks the YAML AST, appending to found the
// dotted path of every mapping key or scalar value containing PlaceholderValue.
// It reads only scalar/key token text, never comment tokens.
func scanPlaceholderNode(n ast.Node, path string, found *[]string) {
	switch node := n.(type) {
	case nil:
		return

	case *ast.MappingNode:
		for _, v := range node.Values {
			scanPlaceholderNode(v, path, found)
		}

	case *ast.MappingValueNode:
		keyStr := placeholderKeyString(node.Key)
		childPath := joinFieldPath(path, keyStr)
		// A placeholder can live in the key itself, e.g.
		// node_groups: {CHANGEME: {...}}. A struct walk misses this; the node
		// walk catches it because the key is a node like any other.
		if strings.Contains(keyStr, PlaceholderValue) {
			*found = append(*found, childPath)
		}
		scanPlaceholderNode(node.Value, childPath, found)

	case *ast.MappingKeyNode: // explicit "? key" form
		scanPlaceholderNode(node.Value, path, found)

	case *ast.SequenceNode:
		for i, item := range node.Values {
			scanPlaceholderNode(item, fmt.Sprintf("%s[%d]", path, i), found)
		}

	case *ast.AnchorNode:
		scanPlaceholderNode(node.Value, path, found)

	case *ast.TagNode:
		scanPlaceholderNode(node.Value, path, found)

	case *ast.LiteralNode:
		// A block scalar's own token is just the "|"/">" indicator; the content
		// hangs off Value. goccy models both literal and folded blocks as
		// LiteralNode, so this covers each of them.
		scanPlaceholderNode(node.Value, path, found)

	default:
		// Scalar leaf (string, int, bool, null, ...). Only its own token text is
		// inspected; comment tokens hang off separate fields and are ignored.
		if tok := n.GetToken(); tok != nil && strings.Contains(tok.Value, PlaceholderValue) {
			*found = append(*found, path)
		}
	}
}

// placeholderKeyString returns the raw text of a mapping key node, used both as
// a path segment and as a value to scan for the sentinel.
func placeholderKeyString(key ast.MapKeyNode) string {
	if key == nil {
		return ""
	}
	if tok := key.GetToken(); tok != nil {
		return tok.Value
	}
	return key.String()
}

// joinFieldPath appends segment to path with a dot separator.
func joinFieldPath(path, segment string) string {
	if path == "" {
		return segment
	}
	return path + "." + segment
}
