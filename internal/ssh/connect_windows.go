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
		_, err := runSession(host, port, user, password, fd)
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
		_, err = runSession(host, port, user, password, fd)
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
		fmt.Fprintf(os.Stderr, "Reconnecting in %s (Enter to retry now)...\r\n", backoff)
		sleepOrEnter(backoff)

		fmt.Fprintf(os.Stderr, "Reconnecting to %s@%s:%d...\r\n", user, host, port)
		start := time.Now()
		connected, err := runSession(host, port, user, password, fd)
		if err == nil || isCleanExit(err) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "Connection lost: %v\r\n", err)

		if connected && time.Since(start) >= sessionStableThreshold {
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

// runSession dials and runs an interactive shell. The first return value
// reports whether Dial succeeded — callers use this to distinguish a failed
// connection (TCP timeout, host down) from a session that connected and then
// dropped, since the two have very different retry semantics.
func runSession(host string, port int, user, password string, fd int) (bool, error) {
	client, err := Dial(host, port, user, password)
	if err != nil {
		return false, err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return true, fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return true, fmt.Errorf("setting raw mode: %w", err)
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
		return true, fmt.Errorf("requesting PTY: %w", err)
	}

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr
	stdin, err := session.StdinPipe()
	if err != nil {
		return true, fmt.Errorf("getting stdin pipe: %w", err)
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

	// Windows has no SIGWINCH; poll the console size and notify the remote on change.
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(resizePollInterval)
		defer ticker.Stop()
		prevW, prevH := width, height
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				w, h, err := term.GetSize(fd)
				if err != nil {
					continue
				}
				if w != prevW || h != prevH {
					session.WindowChange(h, w)
					prevW, prevH = w, h
				}
			}
		}
	}()

	stdinDone := make(chan struct{})
	go func() {
		io.Copy(stdin, os.Stdin)
		stdin.Close()
		close(stdinDone)
	}()
	defer interruptStdinReader(stdinDone)

	if err := session.Shell(); err != nil {
		return true, fmt.Errorf("starting shell: %w", err)
	}
	return true, session.Wait()
}

func isCleanExit(err error) bool {
	var exitErr *ssh.ExitError
	return errors.As(err, &exitErr)
}

const (
	maxAutoRetryDuration   = 1 * time.Minute
	sessionStableThreshold = 10 * time.Second
	resizePollInterval     = 500 * time.Millisecond
)

func nextBackoff(current time.Duration) time.Duration {
	const maxBackoff = 30 * time.Second
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
