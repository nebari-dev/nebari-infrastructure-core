package main

import (
	"errors"
	"fmt"
	"os"
)

// defaultConfigFilename is the name of the config file auto-discovered by NIC.
const defaultConfigFilename = "config.yaml"

// envConfigPath is the environment variable that can override the config file path.
const envConfigPath = "NIC_CONFIG_PATH"

// resolveConfigFile determines the effective config file path using the following priority:
//
//  1. Explicit --file / -f flag (non-empty flagValue)
//  2. NIC_CONFIG_PATH environment variable
//  3. Auto-discovery: ./config.yaml in the current working directory
//
// In all cases the resolved path is verified to be readable before it is
// returned.  An error is returned when no path was explicitly supplied and
// config.yaml is not present in the current directory, or when the resolved
// file exists but cannot be read (e.g. incorrect permissions).
func resolveConfigFile(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, checkReadable(flagValue)
	}

	if envPath := os.Getenv(envConfigPath); envPath != "" {
		return envPath, checkReadable(envPath)
	}

	if fileExists(defaultConfigFilename) {
		// The file is present; make sure we can actually read it before
		// reporting it as the resolved path.
		return defaultConfigFilename, checkReadable(defaultConfigFilename)
	}

	return "", fmt.Errorf(
		"no config file found: provide --file/-f, set %s, or place a %s in the current directory",
		envConfigPath, defaultConfigFilename,
	)
}

// checkReadable verifies that path refers to a file that the current process
// can open for reading.  It is a lightweight pre-flight check that surfaces
// permission errors (or other OS-level access problems) with a clear message
// before any parsing is attempted.
func checkReadable(path string) error {
	// G304: path is intentionally user-supplied (CLI flag, env var, or cwd lookup).
	// The purpose of this function is precisely to open that path as a pre-flight
	// check, so the variable file inclusion is expected.
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return fmt.Errorf("cannot read config file %q: %w", path, err)
	}
	_ = f.Close()
	return nil
}

// fileExists reports whether path points to a regular file (not a directory).
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	return err == nil && !info.IsDir()
}
