package git

import (
	"fmt"
	"net"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/skeema/knownhosts"
	cryptossh "golang.org/x/crypto/ssh"
)

// Auth carries resolved git credentials for a remote repository
type Auth struct {
	token  string
	sshKey string

	// insecureSkipHostKeyVerification disables SSH host key verification,
	// removing protection against man-in-the-middle attacks. Only intended
	// for ephemeral environments (e.g. CI) where maintaining a known_hosts
	// file is impractical. Has no effect on token (HTTPS) authentication.
	insecureSkipHostKeyVerification bool
}

// NewAuthToken creates an Auth object with a token used as the password.
func NewAuthToken(token string) Auth { return Auth{token: token} }

// NewSSHKeyAuth creates an Auth object with an ssh key used for authentication.
// Host keys are verified against the standard known_hosts files unless
// insecureSkipHostKeyVerification is set.
func NewSSHKeyAuth(key string, insecureSkipHostKeyVerification bool) Auth {
	return Auth{sshKey: key, insecureSkipHostKeyVerification: insecureSkipHostKeyVerification}
}

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

		if a.insecureSkipHostKeyVerification {
			return &ssh.PublicKeys{
				User:   "git",
				Signer: signer,
				HostKeyCallbackHelper: ssh.HostKeyCallbackHelper{
					HostKeyCallback: cryptossh.InsecureIgnoreHostKey(), //nolint:gosec // G106: explicit opt-in via insecure_skip_host_key_verification
				},
			}, nil
		}

		callback, err := newHostKeyCallback()
		if err != nil {
			return nil, err
		}
		return &ssh.PublicKeys{
			User:   "git",
			Signer: signer,
			HostKeyCallbackHelper: ssh.HostKeyCallbackHelper{
				HostKeyCallback: callback,
			},
		}, nil
	case a.token != "":
		return &http.BasicAuth{Username: "git", Password: a.token}, nil
	default:
		return nil, nil
	}
}

// newHostKeyCallback returns a host key callback backed by the standard
// known_hosts files (SSH_KNOWN_HOSTS, ~/.ssh/known_hosts, /etc/ssh/ssh_known_hosts),
// wrapping verification failures with actionable guidance.
func newHostKeyCallback() (cryptossh.HostKeyCallback, error) {
	callback, err := ssh.NewKnownHostsCallback()
	if err != nil {
		return nil, fmt.Errorf("ssh host key verification requires a known_hosts file: %w\n"+
			"connect to the git host once with your SSH client (e.g. `ssh git@github.com`) to record its key, "+
			"or set insecure_skip_host_key_verification: true under the repository auth to disable verification (not recommended)", err)
	}

	return func(hostname string, remote net.Addr, key cryptossh.PublicKey) error {
		err := callback(hostname, remote, key)
		host := strings.TrimSuffix(hostname, ":22")
		switch {
		case err == nil:
			return nil
		case knownhosts.IsHostUnknown(err):
			return fmt.Errorf("ssh host key verification failed: %s is not in known_hosts\n"+
				"to trust this host, connect to it once with your SSH client (e.g. `ssh git@%s`) and accept its key, "+
				"or set insecure_skip_host_key_verification: true under the repository auth to disable verification (not recommended)", host, host)
		case knownhosts.IsHostKeyChanged(err):
			return fmt.Errorf("ssh host key verification failed: the key presented by %s does not match known_hosts\n"+
				"this could indicate a man-in-the-middle attack; if the host key legitimately changed, "+
				"remove the old entry (`ssh-keygen -R %s`) and connect once to record the new one: %w", host, host, err)
		default:
			return err
		}
	}, nil
}
