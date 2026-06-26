package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra/doc"
)

func TestGendocsGeneratesExpectedFiles(t *testing.T) {
	tmpDir := t.TempDir()

	rootCmd.RemoveCommand(gendocsCmd)
	t.Cleanup(func() { rootCmd.AddCommand(gendocsCmd) })

	if err := doc.GenMarkdownTree(rootCmd, tmpDir); err != nil {
		t.Fatalf("GenMarkdownTree: %v", err)
	}

	for _, name := range []string{
		"nic.md",
		"nic_deploy.md",
		"nic_destroy.md",
		"nic_validate.md",
		"nic_version.md",
		"nic_kubeconfig.md",
	} {
		if _, err := os.Stat(filepath.Join(tmpDir, name)); err != nil {
			t.Errorf("expected file %s not generated: %v", name, err)
		}
	}
}

func TestGendocsDeployFlagsDocumented(t *testing.T) {
	tmpDir := t.TempDir()

	rootCmd.RemoveCommand(gendocsCmd)
	t.Cleanup(func() { rootCmd.AddCommand(gendocsCmd) })

	if err := doc.GenMarkdownTree(rootCmd, tmpDir); err != nil {
		t.Fatalf("GenMarkdownTree: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tmpDir, "nic_deploy.md"))
	if err != nil {
		t.Fatalf("read nic_deploy.md: %v", err)
	}

	for _, flag := range []string{"--file", "--dry-run", "--timeout", "--regen-apps"} {
		if !bytes.Contains(content, []byte(flag)) {
			t.Errorf("flag %s missing from nic_deploy.md", flag)
		}
	}
}
