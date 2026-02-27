package hetzner

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
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

	// Reuse existing keys
	if _, err := os.Stat(privPath); err == nil {
		if _, err := os.Stat(pubPath); err == nil {
			return pubPath, privPath, nil
		}
	}

	// Generate new ed25519 key pair
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate SSH key: %w", err)
	}

	// Marshal private key to PEM
	privKeyBytes, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privKeyBytes,
	})
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
