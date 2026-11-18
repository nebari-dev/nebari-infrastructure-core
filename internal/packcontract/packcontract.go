// Package packcontract checks the pack platform contract reference
// (docs/pack-contract, ADR-0023) against the rules its policy pages set, so
// that CI enforces them instead of relying on people to remember them.
//
// Each promise page starts with YAML front matter recording its level, its
// state, when it was built, its conformance test and a record of every change
// to its protected text. Check verifies that the record, the page, the README
// table and the ADR table agree, and that the level rules hold.
package packcontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	contractDir = "docs/pack-contract"
	readmePath  = contractDir + "/README.md"
	adrPath     = "docs/adr/0023-pack-platform-contract.md"
	testDir     = "tests/user_journeys/journeys/pack_contract/"
	dateLayout  = "2006-01-02"

	levelStable       = "stable"
	levelExperimental = "experimental"
	statePlanned      = "planned"
	stateBuilt        = "built"

	// graduationMonths is how long a built experimental promise has to
	// graduate, be renewed or be deprecated (policy/proposals.md).
	graduationMonths = 18
)

// sections are the headings every promise page has, in order.
var sections = []string{
	"Promise",
	"Level",
	"State",
	"Interface",
	"Packs may rely on",
	"Packs must not rely on",
	"Per-target notes",
	"Conformance test",
}

// protectedSections are the sections whose text is the promise itself. A
// change to any of them must be recorded in the page's changes list.
var protectedSections = []string{
	"Promise",
	"Level",
	"Interface",
	"Packs may rely on",
	"Packs must not rely on",
}

// states are ordered longest first so prefix matching picks "partly built"
// over "built".
var states = []string{"partly built", "planned", "built"}

// exceptions are the compatibility exceptions in policy/compatibility.md.
var exceptions = map[string]bool{
	"security":       true,
	"unspecified":    true,
	"contract-error": true,
	"bug":            true,
	"upstream-eol":   true,
}

// skipMarkers are the pytest markers and calls that make a journey skip on
// some targets. A conformance test must run on every target.
var skipMarkers = []string{
	"pytest.mark.tls",
	"pytest.mark.requires_trusted_ca",
	"pytest.mark.skip",
	"pytest.mark.xfail",
	"pytest.skip(",
}

var (
	pagePattern    = regexp.MustCompile(`^p[0-9][0-9]-.*\.md$`)
	rowPattern     = regexp.MustCompile(`^\| (P[0-9]+) \|`)
	versionPattern = regexp.MustCompile("Contract version: `([0-9]+)\\.[0-9]+\\.[0-9]+`")
	rfdPattern     = regexp.MustCompile(`^https://github\.com/nebari-dev/governance/issues/[0-9]+$`)
)

type frontMatter struct {
	Promise         string    `yaml:"promise"`
	Level           string    `yaml:"level"`
	State           string    `yaml:"state"`
	Built           string    `yaml:"built"`
	ConformanceTest string    `yaml:"conformance_test"`
	Deprecated      string    `yaml:"deprecated"`
	BoundTo         string    `yaml:"bound_to"`
	Renewals        []renewal `yaml:"renewals"`
	Changes         []change  `yaml:"changes"`
}

type renewal struct {
	Date string `yaml:"date"`
	RFD  string `yaml:"rfd"`
}

// change records one change to a page's protected text. Kind is founding (the
// first version, once), compatible, incompatible or graduation.
type change struct {
	Date      string `yaml:"date"`
	Kind      string `yaml:"kind"`
	RFD       string `yaml:"rfd"`
	Exception string `yaml:"exception"`
	Hash      string `yaml:"hash"`
}

// tableRow is a promise's level and state as a summary table lists them.
type tableRow struct {
	level string
	state string
}

// Check reads the contract reference from fsys, which is rooted at the
// repository root, and returns every rule it breaks. now is the date the
// time-based rules are checked against.
func Check(fsys fs.FS, now time.Time) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	readme, err := fs.ReadFile(fsys, readmePath)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", readmePath, err)}
	}
	adr, err := fs.ReadFile(fsys, adrPath)
	if err != nil {
		return []string{fmt.Sprintf("%s: %v", adrPath, err)}
	}
	for _, link := range []string{"do-not-rely-on.md", "provided-not-promised.md", "not-yet.md"} {
		if !bytes.Contains(readme, []byte(link)) {
			add("README.md: missing link to %s", link)
		}
	}
	major := -1
	if m := versionPattern.FindSubmatch(readme); m != nil {
		_, _ = fmt.Sscanf(string(m[1]), "%d", &major)
	} else {
		add("README.md: no \"Contract version: `X.Y.Z`\" line")
	}
	readmeRows := tableRows(string(readme))
	adrRows := tableRows(string(adr))

	err = fs.WalkDir(fsys, contractDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return err
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		if bytes.ContainsRune(data, '\u2014') {
			add("%s: contains an em dash", p)
		}
		return nil
	})
	if err != nil {
		add("%s: %v", contractDir, err)
	}

	entries, err := fs.ReadDir(fsys, contractDir)
	if err != nil {
		return append(problems, fmt.Sprintf("%s: %v", contractDir, err))
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !pagePattern.MatchString(e.Name()) {
			continue
		}
		p := path.Join(contractDir, e.Name())
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			add("%s: %v", p, err)
			continue
		}
		id := "P" + strings.TrimLeft(e.Name()[1:3], "0")
		seen[id] = true
		for _, msg := range checkPage(fsys, id, string(data), readmeRows, adrRows, major, now) {
			add("%s: %s", e.Name(), msg)
		}
	}
	for _, rows := range []struct {
		name string
		rows map[string]tableRow
	}{{"README.md", readmeRows}, {"ADR-0023", adrRows}} {
		for _, id := range sortedKeys(rows.rows) {
			if !seen[id] {
				add("%s: lists %s but there is no page for it", rows.name, id)
			}
		}
	}
	return problems
}

func checkPage(fsys fs.FS, id, text string, readmeRows, adrRows map[string]tableRow, major int, now time.Time) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	raw, body, ok := splitFrontMatter(text)
	if !ok {
		return []string{"no YAML front matter (see AGENTS.md \"Pack Platform Contract\")"}
	}
	var fm frontMatter
	dec := yaml.NewDecoder(strings.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil {
		return []string{fmt.Sprintf("front matter: %v", err)}
	}

	if fm.Promise != id {
		add("front matter promise is %q, the file name says %s", fm.Promise, id)
	}
	if fm.Level != levelStable && fm.Level != levelExperimental {
		add("front matter level must be stable or experimental, got %q", fm.Level)
	}
	if !contains(states, fm.State) {
		add("front matter state must be planned, partly built or built, got %q", fm.State)
	}

	headings, content := splitSections(body)
	var order []string
	for _, h := range headings {
		if contains(sections, h) {
			order = append(order, h)
		}
	}
	missing := false
	for _, s := range sections {
		if _, ok := content[s]; !ok {
			add("missing section %q", s)
			missing = true
		}
	}
	if !missing && strings.Join(order, "|") != strings.Join(sections, "|") {
		add("sections out of order, expected %s", strings.Join(sections, ", "))
	}

	if level := firstWord(content["Level"]); level != fm.Level {
		add("Level section says %q, front matter says %q", level, fm.Level)
	}
	if state := statePrefix(content["State"]); state != fm.State {
		add("State section says %q, front matter says %q", state, fm.State)
	}
	for _, t := range []struct {
		name string
		rows map[string]tableRow
	}{{"README.md", readmeRows}, {"ADR-0023", adrRows}} {
		row, ok := t.rows[id]
		switch {
		case !ok:
			add("%s has no row for %s", t.name, id)
		case row.level != fm.Level || row.state != fm.State:
			add("%s row says %s/%s, front matter says %s/%s", t.name, row.level, row.state, fm.Level, fm.State)
		}
	}

	// A promise bound to one product must say so where packs read its
	// interface, and name the way to a neutral one (policy/proposals.md).
	if fm.BoundTo != "" {
		switch {
		case !strings.Contains(content["Interface"], "Bound to "+fm.BoundTo):
			add("bound_to is %s but the Interface section has no \"Bound to %s\"", fm.BoundTo, fm.BoundTo)
		case !strings.Contains(content["Interface"], "Path to a neutral interface"):
			add("bound_to is %s but the Interface section names no \"Path to a neutral interface\"", fm.BoundTo)
		}
	}

	hash, _ := protectedHash(body)
	problems = append(problems, checkChanges(fm, hash, now)...)
	problems = append(problems, checkClock(fm, now)...)
	problems = append(problems, checkConformance(fsys, fm, major)...)
	return problems
}

func checkChanges(fm frontMatter, hash string, now time.Time) []string {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if len(fm.Changes) == 0 {
		return []string{"changes is empty; the first entry is the founding record"}
	}
	var prev time.Time
	for i, c := range fm.Changes {
		where := fmt.Sprintf("change %d (%s)", i+1, c.Date)
		d, err := time.Parse(dateLayout, c.Date)
		if err != nil {
			add("%s: date must be YYYY-MM-DD", where)
		} else {
			if d.After(now) {
				add("%s: date is in the future", where)
			}
			if d.Before(prev) {
				add("%s: changes must be in date order", where)
			}
			prev = d
		}
		switch c.Kind {
		case "founding":
			if i != 0 {
				add("%s: only the first change may be founding", where)
			}
		case "compatible", "incompatible", "graduation":
		default:
			add("%s: kind must be founding, compatible, incompatible or graduation, got %q", where, c.Kind)
		}
		// Changes to experimental promises need only a pull request
		// (policy/deprecation.md); stable promises and graduations need an RFD.
		needsRFD := c.Kind == "graduation" || (c.Kind != "founding" && fm.Level == levelStable)
		if needsRFD && !rfdPattern.MatchString(c.RFD) {
			add("%s: needs an rfd link (https://github.com/nebari-dev/governance/issues/N)", where)
		}
		if c.Kind == "graduation" && fm.Level != levelStable {
			add("%s: graduation but the level is %s", where, fm.Level)
		}
		if c.Kind == "incompatible" && fm.Level == levelStable && fm.State != statePlanned && !exceptions[c.Exception] {
			add("%s: an incompatible change to a built stable promise needs a compatibility exception (security, unspecified, contract-error, bug or upstream-eol)", where)
		}
	}
	if last := fm.Changes[len(fm.Changes)-1]; last.Hash != hash {
		add("protected sections changed (%s); record the change in changes with hash: %s", strings.Join(protectedSections, ", "), hash)
	}
	return problems
}

func checkClock(fm frontMatter, now time.Time) []string {
	if fm.Level != levelExperimental || fm.State != stateBuilt {
		return nil
	}
	var problems []string
	for i, r := range fm.Renewals {
		if !rfdPattern.MatchString(r.RFD) {
			problems = append(problems, fmt.Sprintf("renewal %d: renewal needs an rfd link", i+1))
		}
	}
	if fm.Built == "" {
		return append(problems, "a built experimental promise needs a built date; its 18-month graduation clock starts there")
	}
	start, err := time.Parse(dateLayout, fm.Built)
	if err != nil {
		return append(problems, "built must be YYYY-MM-DD")
	}
	for _, r := range fm.Renewals {
		if d, err := time.Parse(dateLayout, r.Date); err == nil && d.After(start) {
			start = d
		}
	}
	if fm.Deprecated != "" {
		if _, err := time.Parse(dateLayout, fm.Deprecated); err != nil {
			problems = append(problems, "deprecated must be YYYY-MM-DD")
		}
		return problems
	}
	if deadline := start.AddDate(0, graduationMonths, 0); now.After(deadline) {
		problems = append(problems, fmt.Sprintf(
			"experimental promise is past its 18-month graduation deadline (%s): graduate it, renew it by RFD, or deprecate it (policy/proposals.md)",
			deadline.Format(dateLayout)))
	}
	return problems
}

func checkConformance(fsys fs.FS, fm frontMatter, major int) []string {
	if fm.State == statePlanned && fm.ConformanceTest == "" {
		return nil
	}
	switch fm.ConformanceTest {
	case "":
		return []string{"a built or partly built promise needs a conformance_test (or pending before contract 1.0)"}
	case "pending":
		if major != 0 {
			return []string{"conformance_test: pending is allowed only before contract 1.0"}
		}
		return nil
	}
	file, fn, ok := strings.Cut(fm.ConformanceTest, "::")
	if !ok {
		return []string{"conformance_test must be <file>::<test function>"}
	}
	if !strings.HasPrefix(file, testDir) {
		return []string{fmt.Sprintf("conformance_test must live under %s", testDir)}
	}
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return []string{fmt.Sprintf("conformance_test %s does not exist", file)}
	}
	var problems []string
	if !bytes.Contains(data, []byte("def "+fn+"(")) {
		problems = append(problems, fmt.Sprintf("conformance_test %s has no function %s", file, fn))
	}
	for _, m := range skipMarkers {
		if bytes.Contains(data, []byte(m)) {
			problems = append(problems, fmt.Sprintf("conformance_test %s uses %s; a conformance test must run on every target", file, m))
		}
	}
	return problems
}

// BoundPromises lists the promises whose interface is bound to one product,
// as "P4: CloudNativePG", in page order. Pages it cannot read or parse are
// skipped; Check reports them.
func BoundPromises(fsys fs.FS) []string {
	entries, err := fs.ReadDir(fsys, contractDir)
	if err != nil {
		return nil
	}
	var bound []string
	for _, e := range entries {
		if e.IsDir() || !pagePattern.MatchString(e.Name()) {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(contractDir, e.Name()))
		if err != nil {
			continue
		}
		raw, _, ok := splitFrontMatter(string(data))
		if !ok {
			continue
		}
		var fm frontMatter
		if err := yaml.Unmarshal([]byte(raw), &fm); err != nil || fm.BoundTo == "" {
			continue
		}
		bound = append(bound, fm.Promise+": "+fm.BoundTo)
	}
	return bound
}

// protectedHash returns a short hash of the protected sections of a page body.
func protectedHash(body string) (string, error) {
	_, content := splitSections(body)
	h := sha256.New()
	for _, s := range protectedSections {
		if _, err := fmt.Fprintf(h, "## %s\n%s\n", s, strings.TrimSpace(content[s])); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

func splitFrontMatter(text string) (front, body string, ok bool) {
	rest, found := strings.CutPrefix(text, "---\n")
	if !found {
		return "", text, false
	}
	front, body, ok = strings.Cut(rest, "\n---\n")
	return front + "\n", body, ok
}

// splitSections returns the "## " headings of body in order, and the text
// under each.
func splitSections(body string) ([]string, map[string]string) {
	var headings []string
	content := map[string]string{}
	current := ""
	var buf strings.Builder
	flush := func() {
		if current != "" {
			content[current] = strings.TrimSpace(buf.String())
		}
		buf.Reset()
	}
	for _, line := range strings.Split(body, "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			current = strings.TrimSpace(h)
			headings = append(headings, current)
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	flush()
	return headings, content
}

// tableRows reads the promise rows (| P1 | name | level | state | ...) of a
// summary table.
func tableRows(text string) map[string]tableRow {
	rows := map[string]tableRow{}
	for _, line := range strings.Split(text, "\n") {
		m := rowPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		cells := strings.Split(strings.Trim(line, "| "), "|")
		if len(cells) < 4 {
			continue
		}
		rows[m[1]] = tableRow{level: strings.TrimSpace(cells[2]), state: strings.TrimSpace(cells[3])}
	}
	return rows
}

func firstWord(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimRight(fields[0], ".,")
}

func statePrefix(s string) string {
	for _, st := range states {
		if s == st || strings.HasPrefix(s, st+".") {
			return st
		}
	}
	return firstWord(s)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]tableRow) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
