package quickstart

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestInstallPacks(t *testing.T) {
	packs := map[string][]byte{
		"nebi-pack.yaml":         []byte("nebi: rendered\n"),
		"data-science-pack.yaml": []byte("dsp: rendered\n"),
	}

	tests := []struct {
		name string
		// change runs between the first and second InstallPacks, as a user
		// would between two quickstart runs.
		change      func(t *testing.T, repoDir string)
		wantCommits int
		wantFiles   map[string]string
	}{
		{
			name:        "rerun with nothing changed makes no commit",
			change:      func(*testing.T, string) {},
			wantCommits: 1,
			wantFiles:   map[string]string{"nebi-pack.yaml": "nebi: rendered\n", "data-science-pack.yaml": "dsp: rendered\n"},
		},
		{
			name: "committed edit is kept",
			change: func(t *testing.T, repoDir string) {
				writeAndCommit(t, repoDir, "nebi-pack.yaml", "nebi: edited\n")
			},
			wantCommits: 2,
			wantFiles:   map[string]string{"nebi-pack.yaml": "nebi: edited\n", "data-science-pack.yaml": "dsp: rendered\n"},
		},
		{
			name: "deleted pack is written again",
			change: func(t *testing.T, repoDir string) {
				removeAndCommit(t, repoDir, "data-science-pack.yaml")
			},
			wantCommits: 3,
			wantFiles:   map[string]string{"nebi-pack.yaml": "nebi: rendered\n", "data-science-pack.yaml": "dsp: rendered\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repoDir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(repoDir, AppsDir), 0o750); err != nil {
				t.Fatal(err)
			}

			if err := InstallPacks(ctx, repoDir, packs); err != nil {
				t.Fatalf("first InstallPacks() error: %v", err)
			}
			if got := commitCount(t, repoDir); got != 1 {
				t.Fatalf("after first run: %d commits, want 1", got)
			}

			tt.change(t, repoDir)

			if err := InstallPacks(ctx, repoDir, packs); err != nil {
				t.Fatalf("second InstallPacks() error: %v", err)
			}
			if got := commitCount(t, repoDir); got != tt.wantCommits {
				t.Errorf("after second run: %d commits, want %d", got, tt.wantCommits)
			}
			for name, want := range tt.wantFiles {
				got, err := os.ReadFile(filepath.Join(repoDir, AppsDir, name)) //nolint:gosec // test-owned temp path
				if err != nil {
					t.Fatalf("read %s: %v", name, err)
				}
				if string(got) != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func commitCount(t *testing.T, repoDir string) int {
	t.Helper()
	repo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	iter, err := repo.Log(&gogit.LogOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	if err := iter.ForEach(func(*object.Commit) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	return n
}

func writeAndCommit(t *testing.T, repoDir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoDir, AppsDir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	commitAll(t, repoDir, "Edit "+name)
}

func removeAndCommit(t *testing.T, repoDir, name string) {
	t.Helper()
	if err := os.Remove(filepath.Join(repoDir, AppsDir, name)); err != nil {
		t.Fatal(err)
	}
	commitAll(t, repoDir, "Remove "+name)
}

func commitAll(t *testing.T, repoDir, message string) {
	t.Helper()
	repo, err := gogit.PlainOpen(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.AddWithOptions(&gogit.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit(message, &gogit.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@example.test"},
	}); err != nil {
		t.Fatal(err)
	}
}
