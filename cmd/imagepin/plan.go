package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
)

// A pin plan says, for each image NIC pins by tag, exactly what to change in
// which file to pin it by digest, in the form that chart accepts. Every
// change is proven by rendering the chart with it before it is printed.
// Nothing is written: applying the plan is a separate, reviewed step.

// location is where NIC's values set one image.
type location struct {
	file string   // values file to edit
	path []string // keys from the file's root to the node
	// kind "map": the node is a map with repository + tag keys.
	// kind "string": the node is a whole image reference.
	kind string
}

func (l location) yamlPath() string { return strings.Join(l.path, ".") }

// form is one way of writing the digest into a values location.
type form struct {
	name  string
	apply func(node map[string]any, key string, tag, digest, name string)
}

var (
	formDigestField = form{"digest field", func(n map[string]any, _ string, _, d, _ string) {
		n["digest"] = d
	}}
	formTagDigest = form{"tag@digest", func(n map[string]any, _ string, tag, d, _ string) {
		n["tag"] = tag + "@" + d
	}}
	formFullRef = form{"full reference", func(n map[string]any, key string, tag, d, name string) {
		n[key] = name + ":" + tag + "@" + d
	}}
)

// change is the planned edit for one image in one chart.
type change struct {
	t         *target
	ref       string // as rendered today
	digest    string // resolved today
	locations []location
	form      form
	err       error  // why no form worked, or why the image wasn't placed
	combined  string // result of the final render with every change applied
}

type plan struct {
	changes  []*change
	direct   []directEdit // images in NIC's own manifests
	moved    []moved      // digest pins whose tag now names a different image
	same     []string     // digest pins whose tag still names the pinned image
	skipped  []skip
	resolved map[string]resolution
}

// moved is a tag@digest pin whose tag now resolves to a different digest:
// the image published under that version changed upstream.
type moved struct {
	ref, pinned, tagNow string
	sources             []string
}

// directEdit is an image written straight into one of NIC's manifests; there
// is no chart in between, so the reference is edited in place.
type directEdit struct {
	file, ref, digest string
}

type skip struct{ ref, why string }

// buildPlan resolves, locates, tries and validates. It renders each chart
// once per attempted form, then once more with all changes together.
func buildPlan(ctx context.Context, r *renderer, res *resolver) *plan {
	p := &plan{resolved: map[string]resolution{}}
	lookup := func(ref string) resolution {
		if x, ok := p.resolved[ref]; ok {
			return x
		}
		x := res.resolve(ctx, ref)
		p.resolved[ref] = x
		return x
	}

	// Classify every inventoried image once.
	for _, ref := range r.inv.sortedRefs() {
		img := r.inv.images[ref]
		switch {
		case !img.setByNIC:
			p.skipped = append(p.skipped, skip{ref, "set by the chart, not NIC: it can't be pinned through NIC's values"})
		case pinStatus(ref) == statusDigest:
			check := r.withRecordedTag(ref)
			name, tag, digest := splitRef(check)
			x := lookup(check)
			switch {
			case x.err != nil:
				p.skipped = append(p.skipped, skip{ref, "pinned by digest, but the lookup failed: " + x.err.Error()})
			case tag == "":
				p.skipped = append(p.skipped, skip{ref, "pinned by digest, and no tag is recorded (not rendered, not in NIC's values), so an upstream change can't be detected"})
			case x.tagNow == "":
				p.skipped = append(p.skipped, skip{ref, "pinned by digest; " + x.drift})
			case x.tagNow != digest:
				p.moved = append(p.moved, moved{ref: name + ":" + tag, pinned: digest, tagNow: x.tagNow, sources: img.sources})
			default:
				p.same = append(p.same, check)
			}
		case pinStatus(ref) != statusTag:
			p.skipped = append(p.skipped, skip{ref, "floating tag: pin a fixed tag first, then convert it"})
		case allManifests(img.sources):
			x := lookup(ref)
			for _, s := range img.sources {
				d := directEdit{file: s, ref: ref, digest: x.digest}
				if x.err != nil {
					d.digest = "ERROR: " + x.err.Error()
				}
				p.direct = append(p.direct, d)
			}
		}
	}

	for _, t := range r.targets {
		baseline, err := r.renderRefs(t, t.values)
		if err != nil {
			continue // already rendered once in run(); can't fail differently
		}
		var mine []*change
		for _, ref := range sortedKeys(baseline.refs) {
			img := r.inv.images[ref]
			if img == nil || !img.setByNIC || pinStatus(ref) != statusTag {
				continue
			}
			c := &change{t: t, ref: ref}
			mine = append(mine, c)
			x := lookup(ref)
			if x.err != nil {
				c.err = fmt.Errorf("resolve: %w", x.err)
				continue
			}
			c.digest = x.digest
			c.locations = findLocations(t, ref)
			if len(c.locations) == 0 {
				c.err = fmt.Errorf("NIC set this reference, but no repository/tag pair or whole-reference value matching it was found to edit")
				continue
			}
			c.form, c.err = r.tryForms(t, c, baseline)
		}

		// Prove the accepted changes together, as they would be applied.
		var ok []*change
		for _, c := range mine {
			if c.err == nil {
				ok = append(ok, c)
			}
		}
		if len(ok) > 0 {
			vals := deepCopy(t.values)
			for _, c := range ok {
				applyForm(vals, c, c.form)
			}
			after, err := r.renderRefs(t, vals)
			for _, c := range ok {
				switch {
				case err != nil:
					c.combined = "FAILED: " + err.Error()
				default:
					if e := verify(after, baseline, c); e != nil {
						c.combined = "FAILED: " + e.Error()
					} else {
						c.combined = "ok"
					}
				}
			}
		}
		p.changes = append(p.changes, mine...)
	}
	return p
}

// withRecordedTag returns ref with its tag restored, when the chart dropped
// the tag from the rendered reference. Charts with a digest field render
// repo@digest; the tag the pin was made from is still in NIC's values, next
// to the digest, and is what the drift check needs. Refs that already carry a
// tag, or whose tag is recorded nowhere, are returned unchanged.
func (r *renderer) withRecordedTag(ref string) string {
	name, tag, digest := splitRef(ref)
	if tag != "" || digest == "" {
		return ref
	}
	for _, t := range r.targets {
		for _, vf := range t.files {
			if found := tagNextTo(vf.values, name, digest); found != "" {
				return name + ":" + found + "@" + digest
			}
		}
	}
	return ref
}

// tagNextTo finds a values map holding this repository and digest, and
// returns the tag recorded beside them.
func tagNextTo(node map[string]any, name, digest string) string {
	if repo, _ := node["repository"].(string); repo != "" && strings.Contains(repo, "/") &&
		strings.HasSuffix(name, repo) && node["digest"] == digest {
		if tag, _ := node["tag"].(string); tag != "" {
			return tag
		}
	}
	for _, v := range node {
		if child, ok := v.(map[string]any); ok {
			if found := tagNextTo(child, name, digest); found != "" {
				return found
			}
		}
	}
	return ""
}

// tryForms returns the first form that renders correctly for this change.
func (r *renderer) tryForms(t *target, c *change, baseline *rendering) (form, error) {
	forms := []form{formDigestField, formTagDigest}
	if c.locations[0].kind == "string" {
		forms = []form{formFullRef}
	}
	var reasons []string
	for _, f := range forms {
		vals := deepCopy(t.values)
		applyForm(vals, c, f)
		after, err := r.renderRefs(t, vals)
		if err == nil {
			err = verify(after, baseline, c)
		}
		if err == nil {
			return f, nil
		}
		reasons = append(reasons, f.name+": "+err.Error())
	}
	return form{}, fmt.Errorf("no form renders correctly: %s", strings.Join(reasons, " | "))
}

// verify checks a render after a change against the render before it: the
// digest is in the rendered reference, the tag-only reference is gone
// everywhere (flags and env vars included), and the change made no label
// invalid. Label problems the chart already had don't count against a change.
func verify(after, before *rendering, c *change) error {
	if after.refs[c.ref] {
		return fmt.Errorf("%s is still rendered: the chart ignored the change somewhere", c.ref)
	}
	name, _, _ := splitRef(c.ref)
	found := false
	for ref := range after.refs {
		n, _, d := splitRef(ref)
		if d == c.digest && normalizeName(n) == normalizeName(name) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no rendered reference to %s carries %s", name, c.digest)
	}
	var introduced []string
	for _, b := range after.badLabels {
		if !before.hasBadLabel(b) {
			introduced = append(introduced, b)
		}
	}
	if len(introduced) > 0 {
		return fmt.Errorf("the change makes labels invalid: %s", capList(introduced, 3))
	}
	return nil
}

func (r *rendering) hasBadLabel(s string) bool {
	for _, b := range r.badLabels {
		if b == s {
			return true
		}
	}
	return false
}

// capList joins at most n items and says how many more there were.
func capList(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, "; ")
	}
	return fmt.Sprintf("%s; and %d more", strings.Join(items[:n], "; "), len(items)-n)
}

func applyForm(vals map[string]any, c *change, f form) {
	name, tag, _ := splitRef(c.ref)
	for _, loc := range c.locations {
		parent, key := walkTo(vals, loc.path)
		if parent == nil {
			continue
		}
		if loc.kind == "string" {
			f.apply(parent, key, tag, c.digest, name)
			continue
		}
		if node, ok := parent[key].(map[string]any); ok {
			f.apply(node, key, tag, c.digest, name)
		}
	}
}

// walkTo returns the map holding the last key of path, and that key.
func walkTo(m map[string]any, path []string) (map[string]any, string) {
	for _, k := range path[:len(path)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			return nil, ""
		}
		m = next
	}
	return m, path[len(path)-1]
}

// findLocations finds where NIC's values files set ref. Where several files
// set the same path, the last one (the one Argo CD applies last) is the one
// to edit.
func findLocations(t *target, ref string) []location {
	name, tag, _ := splitRef(ref)
	byPath := map[string]location{}
	for _, vf := range t.files {
		var visit func(node map[string]any, path []string)
		visit = func(node map[string]any, path []string) {
			if repo, _ := node["repository"].(string); repo != "" && strings.Contains(repo, "/") &&
				strings.HasSuffix(name, repo) && node["tag"] == tag {
				l := location{file: vf.source, path: append([]string{}, path...), kind: "map"}
				byPath[l.yamlPath()] = l
			}
			for k, v := range node {
				p := append(append([]string{}, path...), k)
				switch child := v.(type) {
				case map[string]any:
					visit(child, p)
				case string:
					if child == ref {
						l := location{file: vf.source, path: p, kind: "string"}
						byPath[l.yamlPath()] = l
					}
				}
			}
		}
		visit(vf.values, nil)
	}
	var out []location
	for _, k := range sortedKeys(byPath) {
		out = append(out, byPath[k])
	}
	return out
}

// rendering is what a chart produced: its image references and any labels
// Kubernetes would reject.
type rendering struct {
	refs      map[string]bool
	badLabels []string
}

func (r *renderer) renderRefs(t *target, values map[string]any) (*rendering, error) {
	docs, err := r.renderDocs(t, values)
	if err != nil {
		return nil, err
	}
	scratch := newInventory()
	out := &rendering{refs: map[string]bool{}}
	for _, name := range sortedKeys(docs) {
		if err := scratch.manifest([]byte(docs[name]), origin{source: name}); err != nil {
			return nil, err
		}
		dec := kyaml.NewYAMLOrJSONDecoder(strings.NewReader(docs[name]), 4096)
		for {
			var doc map[string]any
			if err := dec.Decode(&doc); err != nil {
				break // EOF; decode errors were already reported above
			}
			for _, b := range docBadLabels(doc, name) {
				if !out.hasBadLabel(b) {
					out.badLabels = append(out.badLabels, b)
				}
			}
		}
	}
	for ref := range scratch.images {
		out.refs[ref] = true
	}
	return out, nil
}

// docBadLabels returns the invalid labels in one rendered document. A CRD is
// skipped: its schema has properties named "labels" that describe label
// fields (with a description and a type), and it deploys no labels itself.
func docBadLabels(doc map[string]any, where string) []string {
	if doc == nil || doc["kind"] == "CustomResourceDefinition" {
		return nil
	}
	return checkLabels(doc, where)
}

// checkLabels validates every labels/matchLabels map in a document, the way
// the API server would.
func checkLabels(v any, where string) []string {
	var bad []string
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if lm, ok := child.(map[string]any); ok && (k == "labels" || k == "matchLabels") {
				for lk, lv := range lm {
					s, _ := lv.(string)
					for _, e := range validation.IsQualifiedName(lk) {
						bad = append(bad, fmt.Sprintf("%s: label key %q: %s", where, lk, e))
					}
					for _, e := range validation.IsValidLabelValue(s) {
						bad = append(bad, fmt.Sprintf("%s: label %s=%q: %s", where, lk, s, e))
					}
				}
				continue
			}
			bad = append(bad, checkLabels(child, where)...)
		}
	case []any:
		for _, child := range t {
			bad = append(bad, checkLabels(child, where)...)
		}
	}
	return bad
}

func allManifests(sources []string) bool {
	for _, s := range sources {
		if !strings.HasPrefix(s, manifestPrefix+"manifests/") {
			return false
		}
	}
	return len(sources) > 0
}

// printUnchanged lists the digest pins that were checked and need nothing:
// each tag still resolves to the pinned digest. It goes last, after
// everything that needs attention.
func (p *plan) printUnchanged(w io.Writer) {
	if len(p.same) == 0 {
		return
	}
	sort.Strings(p.same)
	_, _ = fmt.Fprintf(w, "\nIMAGE UNCHANGED (%d)\n", len(p.same))
	_, _ = fmt.Fprintln(w, "  Pinned by digest, and the tag still resolves to that digest. Nothing to do.")
	for _, ref := range p.same {
		_, _ = fmt.Fprintf(w, "  - %s\n", ref)
	}
}

// print writes the plan as a review sheet grouped by the file to edit.
// It returns false if anything couldn't be resolved, placed or validated.
func (p *plan) print(w io.Writer) bool {
	ok := true
	_, _ = fmt.Fprintf(w, "DIGEST PIN PLAN  (digests resolved %s)\n", time.Now().UTC().Format("2006-01-02 15:04 MST"))
	_, _ = fmt.Fprintln(w, "Each digest is what its tag pointed to at the time above.")
	_, _ = fmt.Fprintln(w, "Keep each tag: it records the version for people; the digest is what gets pulled.")

	byFile := map[string][]string{}
	for _, c := range p.changes {
		if c.err != nil {
			ok = false
			continue
		}
		name, tag, _ := splitRef(c.ref)
		for _, loc := range c.locations {
			var b strings.Builder
			fmt.Fprintf(&b, "  %s    # %s, %s\n", loc.yamlPath()+":", c.ref, c.form.name)
			switch c.form.name {
			case formDigestField.name:
				fmt.Fprintf(&b, "    tag: %q\n    digest: %q\n", tag, c.digest)
			case formTagDigest.name:
				fmt.Fprintf(&b, "    tag: %q\n", tag+"@"+c.digest)
			case formFullRef.name:
				b.Reset()
				fmt.Fprintf(&b, "  %s: %q    # full reference\n", loc.yamlPath(), name+":"+tag+"@"+c.digest)
			}
			if c.combined != "ok" {
				ok = false
			}
			fmt.Fprintf(&b, "    # check: rendered alone ok; rendered with this file's other changes: %s\n", c.combined)
			byFile[loc.file] = append(byFile[loc.file], b.String())
		}
	}
	for _, d := range p.direct {
		if strings.HasPrefix(d.digest, "ERROR") {
			ok = false
		}
		byFile[d.file] = append(byFile[d.file], fmt.Sprintf(
			"  replace: %s\n     with: %s@%s\n    # NIC's own manifest: edit the reference in place (no chart involved)\n", d.ref, d.ref, d.digest))
	}

	files := sortedKeys(byFile)
	for _, f := range files {
		_, _ = fmt.Fprintf(w, "\n%s\n", f)
		for _, s := range byFile[f] {
			_, _ = fmt.Fprint(w, s)
		}
	}

	var failed []*change
	for _, c := range p.changes {
		if c.err != nil {
			failed = append(failed, c)
		}
	}
	if len(failed) > 0 {
		_, _ = fmt.Fprintf(w, "\nNOT PLANNED (%d)\n", len(failed))
		for _, c := range failed {
			_, _ = fmt.Fprintf(w, "  - %s\n      %s\n      %v\n", c.ref, c.t.label, c.err)
		}
	}

	if len(p.moved) > 0 {
		_, _ = fmt.Fprintf(w, "\nTAG MOVED UPSTREAM (%d)\n", len(p.moved))
		_, _ = fmt.Fprintln(w, "  The tag now names a different image than the pinned digest: the image for that")
		_, _ = fmt.Fprintln(w, "  version changed upstream. The pinned digest is still what gets pulled.")
		for _, m := range p.moved {
			_, _ = fmt.Fprintf(w, "  - %s\n      pinned:  %s\n      tag now: %s\n", m.ref, m.pinned, m.tagNow)
			for _, s := range m.sources {
				_, _ = fmt.Fprintf(w, "      found in %s\n", s)
			}
		}
	}

	if len(p.skipped) > 0 {
		sort.Slice(p.skipped, func(i, j int) bool { return p.skipped[i].ref < p.skipped[j].ref })
		_, _ = fmt.Fprintf(w, "\nNOT IN THIS PLAN (%d)\n", len(p.skipped))
		for _, s := range p.skipped {
			_, _ = fmt.Fprintf(w, "  - %s\n      %s\n", s.ref, s.why)
		}
	}
	return ok
}
