package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var (
	gendocsOutputDir string

	gendocsCmd = &cobra.Command{
		Use:    "__gendocs",
		Short:  "Generate markdown reference documentation",
		Hidden: true,
		RunE:   runGendocs,
	}
)

func init() {
	gendocsCmd.Flags().StringVar(&gendocsOutputDir, "output-dir", "docs/reference/cli", "Directory to write generated markdown files")
	rootCmd.AddCommand(gendocsCmd)
}

func runGendocs(cmd *cobra.Command, args []string) error {
	if err := os.MkdirAll(gendocsOutputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Remove this command from rootCmd so it is excluded from the generated output.
	rootCmd.RemoveCommand(cmd)

	return doc.GenMarkdownTree(rootCmd, gendocsOutputDir)
}
