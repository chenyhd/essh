package ssh

import (
	"net"
	"os"
	"path/filepath"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func authMethods(password string) []gossh.AuthMethod {
	methods := make([]gossh.AuthMethod, 0, 3)

	if signers := loadSigners(); len(signers) > 0 {
		methods = append(methods, gossh.PublicKeys(signers...))
	}

	methods = append(methods,
		gossh.RetryableAuthMethod(gossh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = password
			}
			return answers, nil
		}), 3),
		gossh.RetryableAuthMethod(gossh.PasswordCallback(func() (string, error) {
			return password, nil
		}), 3),
	)

	return methods
}

func loadSigners() []gossh.Signer {
	signers := loadAgentSigners()
	signers = append(signers, loadDefaultKeySigners()...)
	return signers
}

func loadAgentSigners() []gossh.Signer {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}

	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	defer conn.Close()

	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		return nil
	}
	return signers
}

func loadDefaultKeySigners() []gossh.Signer {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	paths := []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
		filepath.Join(home, ".ssh", "id_rsa"),
	}

	signers := make([]gossh.Signer, 0, len(paths))
	for _, path := range paths {
		key, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		signer, err := gossh.ParsePrivateKey(key)
		if err != nil {
			continue
		}
		signers = append(signers, signer)
	}
	return signers
}
