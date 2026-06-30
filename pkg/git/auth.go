package git

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	cryptossh "golang.org/x/crypto/ssh"
)

// Auth carries resolved git credentials for a remote repository
type Auth struct {
	token  string
	sshKey string
}

// NewAuthToken creates an Auth object with a token used as the password.
func NewAuthToken(token string) Auth { return Auth{token: token} }

// NewSSHKeyAuth creates an Auth object with an ssh key used for authentication
func NewSSHKeyAuth(key string) Auth { return Auth{sshKey: key} }

// authType returns "ssh", "token", or "none", for tracing.
func (a Auth) authType() string {
	switch {
	case a.sshKey != "":
		return "ssh"
	case a.token != "":
		return "token"
	default:
		return "none"
	}
}

// method builds the go-git transport.AuthMethod from the resolved credentials.
// It returns nil (no error) when no credentials are set, which go-git treats as
// anonymous access.
func (a Auth) method() (transport.AuthMethod, error) {
	switch {
	case a.sshKey != "":
		signer, err := cryptossh.ParsePrivateKey([]byte(a.sshKey))
		if err != nil {
			return nil, fmt.Errorf("failed to parse SSH private key: %w", err)
		}
		return &ssh.PublicKeys{
			User:   "git",
			Signer: signer,
			// Accept any host key. This is appropriate for automated systems where the
			// configured repository URL is trusted and known_hosts may be absent.
			HostKeyCallbackHelper: ssh.HostKeyCallbackHelper{
				HostKeyCallback: cryptossh.InsecureIgnoreHostKey(), //nolint:gosec // G106: intentional for automated CI/CD systems
			},
		}, nil
	case a.token != "":
		return &http.BasicAuth{Username: "git", Password: a.token}, nil
	default:
		return nil, nil
	}
}
