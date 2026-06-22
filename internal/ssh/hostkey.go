package ssh

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// hostKeyCallback verifies the server's host key against ~/.ssh/known_hosts.
// An unknown host is trusted on first use and its key is recorded; a key that
// no longer matches a recorded one is rejected, since that may indicate a
// man-in-the-middle. This replaces ssh.InsecureIgnoreHostKey, which accepted
// any key without verification.
func hostKeyCallback() (gossh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(sshDir, "known_hosts")
	// knownhosts.New fails if the file is missing, so make sure it exists.
	if f, err := os.OpenFile(path, os.O_CREATE, 0o600); err == nil {
		f.Close()
	}

	verify, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}

	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		err := verify(hostname, remote, key)
		if err == nil {
			return nil
		}

		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) == 0 {
				// Host not seen before: trust on first use and remember it.
				return appendKnownHost(path, hostname, remote, key)
			}
			// A key is already on file but does not match: refuse to connect.
			return fmt.Errorf(
				"host key mismatch for %s (possible man-in-the-middle); "+
					"if you trust this host, remove its entry from %s and retry",
				hostname, path)
		}
		return err
	}, nil
}

// appendKnownHost records a newly-seen host key in the known_hosts file.
func appendKnownHost(path, hostname string, remote net.Addr, key gossh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	addrs := []string{knownhosts.Normalize(hostname)}
	if remote != nil {
		if r := knownhosts.Normalize(remote.String()); r != addrs[0] {
			addrs = append(addrs, r)
		}
	}

	if _, err := f.WriteString(knownhosts.Line(addrs, key) + "\n"); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Warning: permanently added host key for %s to known_hosts.\r\n", hostname)
	return nil
}
