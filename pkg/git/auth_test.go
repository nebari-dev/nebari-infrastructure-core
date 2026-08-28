package git

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

// testSSHKey is a valid Ed25519 SSH private key used to exercise SSH auth.
const testSSHKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBHK2Ow5CDgDQ8L4K2lR8/RZn0J7X9Y5Z5sxQnl5lMaVwAAAJDxAYQo8QGE
KAAAAAtzc2gtZWQyNTUxOQAAACBHK2Ow5CDgDQ8L4K2lR8/RZn0J7X9Y5Z5sxQnl5lMaVw
AAAEBB6qz6RjmJ3M8pLqLyS7X8EXC+xf9lxhJwJzPlJ5OiCUcrY7DkIOANDwvgraVHz9Fm
fQntf1jlnmzFCeXmUxpXAAAADHRlc3RAZXhhbXBsZQE=
-----END OPENSSH PRIVATE KEY-----`

// TestMain points host key verification at a hermetic empty known_hosts file
// so package tests don't depend on the machine they run on. Individual tests
// override SSH_KNOWN_HOSTS via t.Setenv as needed.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "git-test-known-hosts-*")
	if err != nil {
		panic(err)
	}
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		panic(err)
	}
	if err := os.Setenv("SSH_KNOWN_HOSTS", path); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// writeTestKnownHosts writes a known_hosts file with the given lines to a
// temp directory and returns its path.
func writeTestKnownHosts(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("failed to write known_hosts: %v", err)
	}
	return path
}

// generateTestHostKey generates an Ed25519 SSH public key for use as a fake
// server host key in tests.
func generateTestHostKey(t *testing.T) cryptossh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	sshPub, err := cryptossh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("failed to convert key: %v", err)
	}
	return sshPub
}

func TestAuthType(t *testing.T) {
	tests := []struct {
		name string
		auth Auth
		want string
	}{
		{"ssh", NewSSHKeyAuth("key", false), "ssh"},
		{"token", NewAuthToken("tok"), "token"},
		{"none", Auth{}, "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.auth.authType(); got != tt.want {
				t.Errorf("authType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthMethod(t *testing.T) {
	t.Run("token returns BasicAuth", func(t *testing.T) {
		m, err := NewAuthToken("ghp_testtoken").method()
		if err != nil {
			t.Fatalf("method() unexpected error: %v", err)
		}
		basic, ok := m.(*http.BasicAuth)
		if !ok {
			t.Fatalf("method() = %T, want *http.BasicAuth", m)
		}
		if basic.Password != "ghp_testtoken" {
			t.Errorf("BasicAuth.Password = %q, want %q", basic.Password, "ghp_testtoken")
		}
	})

	t.Run("ssh returns PublicKeys", func(t *testing.T) {
		m, err := NewSSHKeyAuth(testSSHKey, false).method()
		if err != nil {
			t.Fatalf("method() unexpected error: %v", err)
		}
		if _, ok := m.(*ssh.PublicKeys); !ok {
			t.Fatalf("method() = %T, want *ssh.PublicKeys", m)
		}
	})

	t.Run("no auth returns nil for anonymous access", func(t *testing.T) {
		m, err := (Auth{}).method()
		if err != nil {
			t.Fatalf("method() unexpected error: %v", err)
		}
		if m != nil {
			t.Errorf("method() = %v, want nil", m)
		}
	})

	t.Run("invalid ssh key returns error", func(t *testing.T) {
		_, err := NewSSHKeyAuth("not-a-valid-key", false).method()
		if err == nil {
			t.Fatal("method() expected error for invalid key, got nil")
		}
		if !strings.Contains(err.Error(), "failed to parse SSH private key") {
			t.Errorf("method() error = %v, want parse error", err)
		}
	})
}

func TestAuthMethodHostKeyVerification(t *testing.T) {
	remote := &net.TCPAddr{IP: net.ParseIP("140.82.112.3"), Port: 22}

	t.Run("fails without known_hosts by default", func(t *testing.T) {
		t.Setenv("SSH_KNOWN_HOSTS", filepath.Join(t.TempDir(), "nonexistent"))

		_, err := NewSSHKeyAuth(testSSHKey, false).method()
		if err == nil {
			t.Fatal("method() expected error when no known_hosts file exists, got nil")
		}
		if !strings.Contains(err.Error(), "known_hosts") {
			t.Errorf("method() error = %v, want guidance mentioning known_hosts", err)
		}
	})

	t.Run("insecure opt-out succeeds without known_hosts", func(t *testing.T) {
		t.Setenv("SSH_KNOWN_HOSTS", filepath.Join(t.TempDir(), "nonexistent"))

		got, err := NewSSHKeyAuth(testSSHKey, true).method()
		if err != nil {
			t.Fatalf("method() unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("method() returned nil auth")
		}
	})

	t.Run("accepts key matching known_hosts", func(t *testing.T) {
		hostKey := generateTestHostKey(t)
		t.Setenv("SSH_KNOWN_HOSTS", writeTestKnownHosts(t,
			"github.com "+strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(hostKey)))))

		callback, err := newHostKeyCallback()
		if err != nil {
			t.Fatalf("newHostKeyCallback() unexpected error: %v", err)
		}
		if err := callback("github.com:22", remote, hostKey); err != nil {
			t.Errorf("callback() unexpected error for matching key: %v", err)
		}
	})

	t.Run("rejects unknown host with guidance", func(t *testing.T) {
		hostKey := generateTestHostKey(t)
		t.Setenv("SSH_KNOWN_HOSTS", writeTestKnownHosts(t,
			"gitlab.com "+strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(hostKey)))))

		callback, err := newHostKeyCallback()
		if err != nil {
			t.Fatalf("newHostKeyCallback() unexpected error: %v", err)
		}
		err = callback("github.com:22", remote, hostKey)
		if err == nil {
			t.Fatal("callback() expected error for unknown host, got nil")
		}
		if !strings.Contains(err.Error(), "not in known_hosts") {
			t.Errorf("callback() error = %v, want message containing %q", err, "not in known_hosts")
		}
		if !strings.Contains(err.Error(), "ssh git@github.com") {
			t.Errorf("callback() error = %v, want remediation hint mentioning %q", err, "ssh git@github.com")
		}
	})

	t.Run("rejects changed host key with MITM warning", func(t *testing.T) {
		recordedKey := generateTestHostKey(t)
		presentedKey := generateTestHostKey(t)
		t.Setenv("SSH_KNOWN_HOSTS", writeTestKnownHosts(t,
			"github.com "+strings.TrimSpace(string(cryptossh.MarshalAuthorizedKey(recordedKey)))))

		callback, err := newHostKeyCallback()
		if err != nil {
			t.Fatalf("newHostKeyCallback() unexpected error: %v", err)
		}
		err = callback("github.com:22", remote, presentedKey)
		if err == nil {
			t.Fatal("callback() expected error for changed host key, got nil")
		}
		if !strings.Contains(err.Error(), "does not match known_hosts") {
			t.Errorf("callback() error = %v, want message containing %q", err, "does not match known_hosts")
		}
		if !strings.Contains(err.Error(), "man-in-the-middle") {
			t.Errorf("callback() error = %v, want message containing %q", err, "man-in-the-middle")
		}
	})
}
