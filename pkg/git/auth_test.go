package git

import (
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// testSSHKey is a valid Ed25519 SSH private key used to exercise SSH auth.
const testSSHKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACBHK2Ow5CDgDQ8L4K2lR8/RZn0J7X9Y5Z5sxQnl5lMaVwAAAJDxAYQo8QGE
KAAAAAtzc2gtZWQyNTUxOQAAACBHK2Ow5CDgDQ8L4K2lR8/RZn0J7X9Y5Z5sxQnl5lMaVw
AAAEBB6qz6RjmJ3M8pLqLyS7X8EXC+xf9lxhJwJzPlJ5OiCUcrY7DkIOANDwvgraVHz9Fm
fQntf1jlnmzFCeXmUxpXAAAADHRlc3RAZXhhbXBsZQE=
-----END OPENSSH PRIVATE KEY-----`

func TestAuthType(t *testing.T) {
	tests := []struct {
		name string
		auth Auth
		want string
	}{
		{"ssh", NewSSHKeyAuth("key"), "ssh"},
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
		m, err := NewSSHKeyAuth(testSSHKey).method()
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
		_, err := NewSSHKeyAuth("not-a-valid-key").method()
		if err == nil {
			t.Fatal("method() expected error for invalid key, got nil")
		}
		if !strings.Contains(err.Error(), "failed to parse SSH private key") {
			t.Errorf("method() error = %v, want parse error", err)
		}
	})
}
