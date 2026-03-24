//go:build !windows

package ssh

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// Connect establishes an SSH connection and starts an interactive shell session.
// If the connection drops unexpectedly, it automatically reconnects with backoff.
func Connect(host string, port int, user, password string) error {
	fd := int(os.Stdin.Fd())
	backoff := time.Second

	for {
		err := runSession(host, port, user, password, fd)
		if err == nil || isCleanExit(err) {
			return nil
		}

		// Ensure terminal is restored before printing
		fmt.Fprintf(os.Stderr, "\r\nConnection lost: %v\r\n", err)

		for {
			fmt.Fprintf(os.Stderr, "Reconnecting in %s...\r\n", backoff)
			time.Sleep(backoff)

			fmt.Fprintf(os.Stderr, "Reconnecting to %s@%s:%d...\r\n", user, host, port)
			err = runSession(host, port, user, password, fd)
			if err == nil || isCleanExit(err) {
				return nil
			}

			fmt.Fprintf(os.Stderr, "Reconnect failed: %v\r\n", err)
			backoff = nextBackoff(backoff)
		}
	}
}

func runSession(host string, port int, user, password string, fd int) error {
	client, err := Dial(host, port, user, password)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("setting raw mode: %w", err)
	}
	defer term.Restore(fd, oldState)

	width, height, err := term.GetSize(fd)
	if err != nil {
		width, height = 80, 24
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("xterm-256color", height, width, modes); err != nil {
		return fmt.Errorf("requesting PTY: %w", err)
	}

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("getting stdin pipe: %w", err)
	}

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

	go func() {
		io.Copy(stdin, os.Stdin)
		stdin.Close()
	}()

	if err := session.Shell(); err != nil {
		return fmt.Errorf("starting shell: %w", err)
	}
	return session.Wait()
}

// isCleanExit returns true if the error represents a normal session exit
// (not a connection drop).
func isCleanExit(err error) bool {
	var exitErr *ssh.ExitError
	return errors.As(err, &exitErr)
}

func nextBackoff(current time.Duration) time.Duration {
	const maxBackoff = 30 * time.Second
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
