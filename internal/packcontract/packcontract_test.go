package packcontract

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// fixture describes a minimal repository with one promise, P1. Each test case
// mutates a valid fixture to break exactly one rule.
type fixture struct {
	contractVersion string
	level           string // front matter
	state           string // front matter
	sectionLevel    string // body of the Level section
	sectionState    string // body of the State section
	readmeLevel     string
	readmeState     string
	adrLevel        string
	adrState        string
	extraFront      string // extra front matter lines (built, conformance_test, renewals, deprecated)
	changes         string // the changes list, with {{hash}} replaced by the current hash
	promiseText     string
	swapSections    bool
	testFile        string // content of the conformance test file; empty means absent
}

func validFixture() fixture {
	return fixture{
		contractVersion: "0.1.0",
		level:           "stable",
		state:           "planned",
		sectionLevel:    "stable",
		sectionState:    "planned. Nothing is built.",
		readmeLevel:     "stable",
		readmeState:     "planned",
		adrLevel:        "stable",
		adrState:        "planned",
		changes:         "  - date: 2026-11-17\n    kind: founding\n    hash: {{hash}}\n",
		promiseText:     "Packs get a thing.",
	}
}

const testPath = "tests/user_journeys/journeys/pack_contract/test_p01_thing.py"

func (f fixture) body() string {
	sections := []string{
		"## Promise\n\n" + f.promiseText,
		"## Level\n\n" + f.sectionLevel,
		"## State\n\n" + f.sectionState,
		"## Interface\n\nA key.",
		"## Packs may rely on\n\n- The key.",
		"## Packs must not rely on\n\n- Anything else.",
		"## Per-target notes\n\nNone.",
		"## Conformance test\n\nIt checks the key.",
	}
	if f.swapSections {
		sections[0], sections[1] = sections[1], sections[0]
	}
	return "# P1: Thing\n\n" + strings.Join(sections, "\n\n") + "\n"
}

func (f fixture) fs(t *testing.T) fstest.MapFS {
	t.Helper()
	body := f.body()
	hash, err := protectedHash(body)
	if err != nil {
		t.Fatalf("hashing fixture: %v", err)
	}
	front := fmt.Sprintf("---\npromise: P1\nlevel: %s\nstate: %s\n%schanges:\n%s---\n",
		f.level, f.state, f.extraFront, strings.ReplaceAll(f.changes, "{{hash}}", hash))
	fsys := fstest.MapFS{
		"docs/pack-contract/README.md": {Data: []byte(fmt.Sprintf(
			"# Pack platform contract\n\nContract version: `%s` (proposed).\n\n"+
				"| # | Promise | Level | State | Page |\n|---|---|---|---|---|\n"+
				"| P1 | Thing | %s | %s | [P1](p01-thing.md) |\n\n"+
				"See [a](do-not-rely-on.md), [b](provided-not-promised.md) and [c](not-yet.md).\n",
			f.contractVersion, f.readmeLevel, f.readmeState))},
		"docs/adr/0023-pack-platform-contract.md": {Data: []byte(fmt.Sprintf(
			"# ADR-0023\n\n## Status\n\nProposed\n\n| # | Promise | Level | State |\n|---|---|---|---|\n| P1 | Thing | %s | %s |\n",
			f.adrLevel, f.adrState))},
		"docs/pack-contract/p01-thing.md": {Data: []byte(front + body)},
	}
	if f.testFile != "" {
		fsys[testPath] = &fstest.MapFile{Data: []byte(f.testFile)}
	}
	return fsys
}

var now = time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)

const goodTest = "def test_p01_thing():\n    assert True\n"

func TestCheck(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(f *fixture)
		want   string // substring of the single expected problem; empty means no problems
	}{
		{name: "valid fixture", mutate: func(f *fixture) {}},
		{
			name:   "front matter level disagrees with the Level section",
			mutate: func(f *fixture) { f.sectionLevel = "experimental" },
			want:   "Level section says \"experimental\"",
		},
		{
			name:   "front matter state disagrees with the State section",
			mutate: func(f *fixture) { f.sectionState = "built. It exists." },
			want:   "State section says \"built\"",
		},
		{
			name:   "README row disagrees",
			mutate: func(f *fixture) { f.readmeState = "built" },
			want:   "README.md row says stable/built",
		},
		{
			name:   "ADR row disagrees",
			mutate: func(f *fixture) { f.adrLevel = "experimental" },
			want:   "ADR-0023 row says",
		},
		{
			name:   "sections out of order",
			mutate: func(f *fixture) { f.swapSections = true },
			want:   "sections out of order",
		},
		{
			name:   "em dash in a page",
			mutate: func(f *fixture) { f.promiseText = "Packs get a thing \u2014 really." },
			want:   "em dash",
		},
		{
			name: "protected text changed without a change record",
			mutate: func(f *fixture) {
				f.changes = "  - date: 2026-11-17\n    kind: founding\n    hash: 0000000000000000\n"
			},
			want: "protected sections changed",
		},
		{
			name: "founding record that is not first",
			mutate: func(f *fixture) {
				f.changes = "  - date: 2026-11-17\n    kind: founding\n    hash: aaaa\n  - date: 2026-11-18\n    kind: founding\n    hash: {{hash}}\n"
			},
			want: "only the first change may be founding",
		},
		{
			name: "change to a stable promise without an RFD",
			mutate: func(f *fixture) {
				f.changes += "  - date: 2026-11-18\n    kind: compatible\n    hash: {{hash}}\n"
				f.changes = strings.Replace(f.changes, "hash: {{hash}}", "hash: aaaa", 1)
			},
			want: "needs an rfd link",
		},
		{
			name: "change to a stable promise with an RFD",
			mutate: func(f *fixture) {
				f.changes = "  - date: 2026-11-17\n    kind: founding\n    hash: aaaa\n" +
					"  - date: 2026-11-18\n    kind: compatible\n    rfd: https://github.com/nebari-dev/governance/issues/70\n    hash: {{hash}}\n"
			},
		},
		{
			name: "change dated in the future",
			mutate: func(f *fixture) {
				f.changes = "  - date: 2026-11-17\n    kind: founding\n    hash: aaaa\n" +
					"  - date: 2026-12-01\n    kind: compatible\n    rfd: https://github.com/nebari-dev/governance/issues/70\n    hash: {{hash}}\n"
			},
			want: "is in the future",
		},
		{
			name: "incompatible change to a built stable promise without an exception",
			mutate: func(f *fixture) {
				f.state, f.sectionState, f.readmeState, f.adrState = "built", "built. It exists.", "built", "built"
				f.extraFront = "conformance_test: pending\n"
				f.changes = "  - date: 2026-11-17\n    kind: founding\n    hash: aaaa\n" +
					"  - date: 2026-11-18\n    kind: incompatible\n    rfd: https://github.com/nebari-dev/governance/issues/70\n    hash: {{hash}}\n"
			},
			want: "needs a compatibility exception",
		},
		{
			name: "incompatible change to a built stable promise under the security exception",
			mutate: func(f *fixture) {
				f.state, f.sectionState, f.readmeState, f.adrState = "built", "built. It exists.", "built", "built"
				f.extraFront = "conformance_test: pending\n"
				f.changes = "  - date: 2026-11-17\n    kind: founding\n    hash: aaaa\n" +
					"  - date: 2026-11-18\n    kind: incompatible\n    exception: security\n    rfd: https://github.com/nebari-dev/governance/issues/70\n    hash: {{hash}}\n"
			},
		},
		{
			name: "incompatible change to an experimental promise needs no RFD or notice",
			mutate: func(f *fixture) {
				f.level, f.sectionLevel, f.readmeLevel, f.adrLevel = "experimental", "experimental", "experimental", "experimental"
				f.changes = "  - date: 2026-11-17\n    kind: founding\n    hash: aaaa\n" +
					"  - date: 2026-11-18\n    kind: incompatible\n    hash: {{hash}}\n"
			},
		},
		{
			name: "graduation without an RFD",
			mutate: func(f *fixture) {
				f.changes = "  - date: 2026-11-17\n    kind: founding\n    hash: aaaa\n" +
					"  - date: 2026-11-18\n    kind: graduation\n    hash: {{hash}}\n"
			},
			want: "needs an rfd link",
		},
		{
			name: "graduation while still experimental",
			mutate: func(f *fixture) {
				f.level, f.sectionLevel, f.readmeLevel, f.adrLevel = "experimental", "experimental", "experimental", "experimental"
				f.changes = "  - date: 2026-11-17\n    kind: founding\n    hash: aaaa\n" +
					"  - date: 2026-11-18\n    kind: graduation\n    rfd: https://github.com/nebari-dev/governance/issues/70\n    hash: {{hash}}\n"
			},
			want: "graduation but the level is experimental",
		},
		{
			name: "built experimental promise without a built date",
			mutate: func(f *fixture) {
				experimentalBuilt(f, "conformance_test: pending\n")
			},
			want: "needs a built date",
		},
		{
			name: "built experimental promise inside its 18 months",
			mutate: func(f *fixture) {
				experimentalBuilt(f, "built: 2025-06-01\nconformance_test: pending\n")
			},
		},
		{
			name: "built experimental promise past its 18 months",
			mutate: func(f *fixture) {
				experimentalBuilt(f, "built: 2025-05-17\nconformance_test: pending\n")
			},
			want: "past its 18-month graduation deadline",
		},
		{
			name: "renewal restarts the clock",
			mutate: func(f *fixture) {
				experimentalBuilt(f, "built: 2024-01-01\nconformance_test: pending\nrenewals:\n  - date: 2026-01-01\n    rfd: https://github.com/nebari-dev/governance/issues/71\n")
			},
		},
		{
			name: "renewal without an RFD",
			mutate: func(f *fixture) {
				experimentalBuilt(f, "built: 2024-01-01\nconformance_test: pending\nrenewals:\n  - date: 2026-01-01\n")
			},
			want: "renewal needs an rfd link",
		},
		{
			name: "deprecation stops the clock",
			mutate: func(f *fixture) {
				experimentalBuilt(f, "built: 2024-01-01\nconformance_test: pending\ndeprecated: 2025-12-01\n")
			},
		},
		{
			name: "built promise without a conformance test",
			mutate: func(f *fixture) {
				f.state, f.sectionState, f.readmeState, f.adrState = "built", "built. It exists.", "built", "built"
			},
			want: "needs a conformance_test",
		},
		{
			name: "pending conformance test is refused at 1.0",
			mutate: func(f *fixture) {
				f.contractVersion = "1.0.0"
				f.state, f.sectionState, f.readmeState, f.adrState = "built", "built. It exists.", "built", "built"
				f.extraFront = "conformance_test: pending\n"
			},
			want: "pending is allowed only before contract 1.0",
		},
		{
			name: "conformance test that exists",
			mutate: func(f *fixture) {
				f.state, f.sectionState, f.readmeState, f.adrState = "built", "built. It exists.", "built", "built"
				f.extraFront = "conformance_test: " + testPath + "::test_p01_thing\n"
				f.testFile = goodTest
			},
		},
		{
			name: "conformance test file missing",
			mutate: func(f *fixture) {
				f.state, f.sectionState, f.readmeState, f.adrState = "built", "built. It exists.", "built", "built"
				f.extraFront = "conformance_test: " + testPath + "::test_p01_thing\n"
			},
			want: "does not exist",
		},
		{
			name: "conformance test function missing",
			mutate: func(f *fixture) {
				f.state, f.sectionState, f.readmeState, f.adrState = "built", "built. It exists.", "built", "built"
				f.extraFront = "conformance_test: " + testPath + "::test_other\n"
				f.testFile = goodTest
			},
			want: "has no function test_other",
		},
		{
			name: "conformance test outside the pack_contract group",
			mutate: func(f *fixture) {
				f.state, f.sectionState, f.readmeState, f.adrState = "built", "built. It exists.", "built", "built"
				f.extraFront = "conformance_test: tests/user_journeys/journeys/test_tls.py::test_x\n"
			},
			want: "must live under tests/user_journeys/journeys/pack_contract/",
		},
		{
			name: "conformance test that skips on some targets",
			mutate: func(f *fixture) {
				f.state, f.sectionState, f.readmeState, f.adrState = "built", "built. It exists.", "built", "built"
				f.extraFront = "conformance_test: " + testPath + "::test_p01_thing\n"
				f.testFile = "import pytest\n\n@pytest.mark.tls\n" + goodTest
			},
			want: "pytest.mark.tls",
		},
		{
			name:   "unknown front matter field",
			mutate: func(f *fixture) { f.extraFront = "colour: blue\n" },
			want:   "field colour not found",
		},
		{
			name:   "invalid level",
			mutate: func(f *fixture) { f.level, f.sectionLevel, f.readmeLevel, f.adrLevel = "beta", "beta", "beta", "beta" },
			want:   "level must be stable or experimental",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := validFixture()
			tc.mutate(&f)
			problems := Check(f.fs(t), now)
			if tc.want == "" {
				if len(problems) != 0 {
					t.Fatalf("want no problems, got:\n%s", strings.Join(problems, "\n"))
				}
				return
			}
			if len(problems) != 1 || !strings.Contains(problems[0], tc.want) {
				t.Fatalf("want one problem containing %q, got:\n%s", tc.want, strings.Join(problems, "\n"))
			}
		})
	}
}

func experimentalBuilt(f *fixture, extra string) {
	f.level, f.sectionLevel, f.readmeLevel, f.adrLevel = "experimental", "experimental", "experimental", "experimental"
	f.state, f.sectionState, f.readmeState, f.adrState = "built", "built. It exists.", "built", "built"
	f.extraFront = extra
}

// TestRepositoryPackContract checks the real contract pages. It is what makes
// CI enforce the rules in docs/pack-contract/policy/: if it fails, read the
// problem, fix the page (or record the change), and see AGENTS.md "Pack
// Platform Contract".
func TestRepositoryPackContract(t *testing.T) {
	if _, err := os.Stat("../../docs/pack-contract/README.md"); err != nil {
		t.Fatalf("pack contract reference not found: %v", err)
	}
	for _, p := range Check(os.DirFS("../.."), time.Now().UTC()) {
		t.Error(p)
	}
}
