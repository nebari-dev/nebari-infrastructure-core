package hetzner

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// ensureSSHKeys returns paths to public and private SSH keys.
// If sshCfg is provided and has paths, those are validated and returned.
// Otherwise, generates an ed25519 key pair in cacheDir, reusing existing keys if present.
func ensureSSHKeys(cacheDir string, sshCfg *SSHConfig) (pubPath, privPath string, err error) {
	// User-provided keys take precedence
	if sshCfg != nil && sshCfg.PublicKeyPath != "" && sshCfg.PrivateKeyPath != "" {
		if _, err := os.Stat(sshCfg.PublicKeyPath); err != nil {
			return "", "", fmt.Errorf("SSH public key not found at %s: %w", sshCfg.PublicKeyPath, err)
		}
		if _, err := os.Stat(sshCfg.PrivateKeyPath); err != nil {
			return "", "", fmt.Errorf("SSH private key not found at %s: %w", sshCfg.PrivateKeyPath, err)
		}
		return sshCfg.PublicKeyPath, sshCfg.PrivateKeyPath, nil
	}

	// Auto-generate keys in cache directory
	sshDir := filepath.Join(cacheDir, "ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return "", "", fmt.Errorf("failed to create SSH key directory: %w", err)
	}

	privPath = filepath.Join(sshDir, "hetzner_ed25519")
	pubPath = filepath.Join(sshDir, "hetzner_ed25519.pub")

	// Reuse existing keys, but error if the pair is incomplete
	privExists := fileExists(privPath)
	pubExists := fileExists(pubPath)
	if privExists && pubExists {
		return pubPath, privPath, nil
	}
	if privExists != pubExists {
		return "", "", fmt.Errorf("SSH key pair is incomplete (private=%v, public=%v) at %s - delete both files to regenerate", privExists, pubExists, sshDir)
	}

	// Generate new ed25519 key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate SSH key: %w", err)
	}

	// Marshal private key to OpenSSH format (BEGIN OPENSSH PRIVATE KEY)
	// PKCS#8 format is not universally supported by SSH clients and tools,
	// especially hetzner-k3s which requires OpenSSH format keys.
	privBlock, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(privBlock)
	if err := os.WriteFile(privPath, privPEM, 0600); err != nil {
		return "", "", fmt.Errorf("failed to write private key: %w", err)
	}

	// Marshal public key to authorized_keys format
	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to create SSH public key: %w", err)
	}
	pubKeyBytes := ssh.MarshalAuthorizedKey(sshPubKey)
	if err := os.WriteFile(pubPath, pubKeyBytes, 0644); err != nil { //nolint:gosec // Public key files are world-readable by convention
		return "", "", fmt.Errorf("failed to write public key: %w", err)
	}

	return pubPath, privPath, nil
}

// fileExists returns true if the path exists and is accessible.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
