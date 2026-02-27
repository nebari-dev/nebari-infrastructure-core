package hetzner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureSSHKeys_GeneratesKeys(t *testing.T) {
	dir := t.TempDir()
	pubPath, privPath, err := ensureSSHKeys(dir, nil)
	if err != nil {
		t.Fatalf("ensureSSHKeys() error = %v", err)
	}

	if _, err := os.Stat(pubPath); err != nil {
		t.Errorf("public key not found at %s", pubPath)
	}
	if _, err := os.Stat(privPath); err != nil {
		t.Errorf("private key not found at %s", privPath)
	}

	// Verify private key has restrictive permissions
	info, _ := os.Stat(privPath)
	if info.Mode().Perm() != 0600 {
		t.Errorf("private key permissions = %o, want 0600", info.Mode().Perm())
	}
}

func TestEnsureSSHKeys_ReusesExisting(t *testing.T) {
	dir := t.TempDir()

	_, priv1, err := ensureSSHKeys(dir, nil)
	if err != nil {
		t.Fatalf("first call error = %v", err)
	}
	content1, _ := os.ReadFile(priv1) //nolint:gosec // Test file, path from t.TempDir()

	_, priv2, err := ensureSSHKeys(dir, nil)
	if err != nil {
		t.Fatalf("second call error = %v", err)
	}
	content2, _ := os.ReadFile(priv2) //nolint:gosec // Test file, path from t.TempDir()

	if priv1 != priv2 {
		t.Error("paths should be identical on second call")
	}
	if string(content1) != string(content2) {
		t.Error("key content should be identical on second call")
	}
}

func TestEnsureSSHKeys_UserOverride(t *testing.T) {
	dir := t.TempDir()

	userPub := filepath.Join(dir, "user.pub")
	userPriv := filepath.Join(dir, "user")
	_ = os.WriteFile(userPub, []byte("ssh-ed25519 AAAA user@test"), 0644) //nolint:gosec // Test file, path from t.TempDir()
	_ = os.WriteFile(userPriv, []byte("private-key-data"), 0600)          // Test file, path from t.TempDir()

	sshCfg := &SSHConfig{
		PublicKeyPath:  userPub,
		PrivateKeyPath: userPriv,
	}

	pubPath, privPath, err := ensureSSHKeys(dir, sshCfg)
	if err != nil {
		t.Fatalf("ensureSSHKeys() error = %v", err)
	}

	if pubPath != userPub {
		t.Errorf("pubPath = %q, want %q", pubPath, userPub)
	}
	if privPath != userPriv {
		t.Errorf("privPath = %q, want %q", privPath, userPriv)
	}
}

func TestEnsureSSHKeys_UserOverrideMissing(t *testing.T) {
	dir := t.TempDir()
	sshCfg := &SSHConfig{
		PublicKeyPath:  "/nonexistent/key.pub",
		PrivateKeyPath: "/nonexistent/key",
	}

	_, _, err := ensureSSHKeys(dir, sshCfg)
	if err == nil {
		t.Error("expected error for missing user SSH keys")
	}
}
