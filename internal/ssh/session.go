package ssh

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// Connect establishes an SSH connection and starts an interactive shell session.
func Connect(host string, port int, user, password string) error {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	// Put local terminal in raw mode
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("setting raw mode: %w", err)
	}
	defer term.Restore(fd, oldState)

	// Get terminal dimensions
	width, height, err := term.GetSize(fd)
	if err != nil {
		width, height = 80, 24
	}

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", height, width, modes); err != nil {
		term.Restore(fd, oldState)
		return fmt.Errorf("requesting PTY: %w", err)
	}

	// Pipe stdin/stdout/stderr
	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	stdin, err := session.StdinPipe()
	if err != nil {
		term.Restore(fd, oldState)
		return fmt.Errorf("getting stdin pipe: %w", err)
	}

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		for sig := range sigCh {
			switch sig {
			case syscall.SIGWINCH:
				w, h, err := term.GetSize(fd)
				if err == nil {
					session.WindowChange(h, w)
				}
			case syscall.SIGINT, syscall.SIGTERM:
				term.Restore(fd, oldState)
				os.Exit(0)
			}
		}
	}()

	// Copy stdin to session
	go func() {
		io.Copy(stdin, os.Stdin)
		stdin.Close()
	}()

	// Start shell and wait
	if err := session.Shell(); err != nil {
		term.Restore(fd, oldState)
		return fmt.Errorf("starting shell: %w", err)
	}
	return session.Wait()
}
