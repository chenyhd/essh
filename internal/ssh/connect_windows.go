//go:build windows

package ssh

import (
	"bufio"
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
// If the connection drops, it auto-reconnects with backoff for up to
// maxAutoRetryDuration; after that it pauses and waits for the user to press
// Enter to retry.
func Connect(host string, port int, user, password string) error {
	fd := int(os.Stdin.Fd())

	for {
		err := runSession(host, port, user, password, fd)
		if err == nil || isCleanExit(err) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "\r\nConnection lost: %v\r\n", err)

		if err := reconnectLoop(host, port, user, password, fd); err != nil {
			return err
		}
	}
}

func reconnectLoop(host string, port int, user, password string, fd int) error {
	for {
		err := autoReconnect(host, port, user, password, fd)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errAutoRetryExhausted) {
			return err
		}

		if err := waitForEnter(host, port, user); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Reconnecting to %s@%s:%d...\r\n", user, host, port)
		err = runSession(host, port, user, password, fd)
		if err == nil || isCleanExit(err) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "Connection lost: %v\r\n", err)
	}
}

var errAutoRetryExhausted = errors.New("auto-retry exhausted")

func autoReconnect(host string, port int, user, password string, fd int) error {
	backoff := time.Second
	deadline := time.Now().Add(maxAutoRetryDuration)

	for time.Now().Before(deadline) {
		fmt.Fprintf(os.Stderr, "Reconnecting in %s...\r\n", backoff)
		time.Sleep(backoff)

		fmt.Fprintf(os.Stderr, "Reconnecting to %s@%s:%d...\r\n", user, host, port)
		start := time.Now()
		err := runSession(host, port, user, password, fd)
		if err == nil || isCleanExit(err) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "Connection lost: %v\r\n", err)

		if time.Since(start) >= sessionStableThreshold {
			backoff = time.Second
			deadline = time.Now().Add(maxAutoRetryDuration)
			continue
		}
		backoff = nextBackoff(backoff)
	}
	return errAutoRetryExhausted
}

func waitForEnter(host string, port int, user string) error {
	fmt.Fprintf(os.Stderr,
		"\r\nAuto-reconnect paused after %s. Press Enter to retry %s@%s:%d, or Ctrl+C to quit.\r\n",
		maxAutoRetryDuration, user, host, port)
	reader := bufio.NewReader(os.Stdin)
	_, err := reader.ReadString('\n')
	return err
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
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	go func() {
		for range sigCh {
			term.Restore(fd, oldState)
			os.Exit(0)
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

func isCleanExit(err error) bool {
	var exitErr *ssh.ExitError
	return errors.As(err, &exitErr)
}

const (
	maxAutoRetryDuration   = 1 * time.Minute
	sessionStableThreshold = 10 * time.Second
)

func nextBackoff(current time.Duration) time.Duration {
	const maxBackoff = 30 * time.Second
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
