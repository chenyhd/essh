//go:build windows

package ssh

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// Connect establishes an SSH connection and starts an interactive session.
// When command is empty an interactive login shell is started; otherwise command
// is run on the remote (e.g. a tmux attach-or-create command). If the connection
// drops, it auto-reconnects with backoff for up to maxAutoRetryDuration; after
// that it pauses and waits for the user to press Enter to retry.
func Connect(host string, port int, user, password, command string) error {
	fd := int(os.Stdin.Fd())
	broker := newStdinBroker()

	for {
		_, err := runSession(broker, host, port, user, password, command, fd)
		if err == nil || isCleanExit(err) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "\r\nConnection lost: %v\r\n", err)

		if err := reconnectLoop(broker, host, port, user, password, command, fd); err != nil {
			return err
		}
	}
}

func reconnectLoop(broker *stdinBroker, host string, port int, user, password, command string, fd int) error {
	for {
		err := autoReconnect(broker, host, port, user, password, command, fd)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errAutoRetryExhausted) {
			return err
		}

		if err := waitForEnter(broker, host, port, user); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Reconnecting to %s@%s:%d...\r\n", user, host, port)
		_, err = runSession(broker, host, port, user, password, command, fd)
		if err == nil || isCleanExit(err) {
			return nil
		}
		fmt.Fprintf(os.Stderr, "Connection lost: %v\r\n", err)
	}
}

var errAutoRetryExhausted = errors.New("auto-retry exhausted")

func autoReconnect(broker *stdinBroker, host string, port int, user, password, command string, fd int) error {
	backoff := time.Second
	deadline := time.Now().Add(maxAutoRetryDuration)

	for time.Now().Before(deadline) {
		fmt.Fprintf(os.Stderr, "Reconnecting in %s (Enter to retry now)...\r\n", backoff)
		broker.sleepOrEnter(backoff)

		fmt.Fprintf(os.Stderr, "Reconnecting to %s@%s:%d...\r\n", user, host, port)
		start := time.Now()
		connected, err := runSession(broker, host, port, user, password, command, fd)
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

func waitForEnter(broker *stdinBroker, host string, port int, user string) error {
	fmt.Fprintf(os.Stderr,
		"\r\nAuto-reconnect paused after %s. Press Enter to retry %s@%s:%d, or Ctrl+C to quit.\r\n",
		maxAutoRetryDuration, user, host, port)
	return broker.waitForLine()
}

// runSession dials and runs an interactive shell. The first return value
// reports whether Dial succeeded — callers use this to distinguish a failed
// connection (TCP timeout, host down) from a session that connected and then
// dropped, since the two have very different retry semantics.
func runSession(broker *stdinBroker, host string, port int, user, password, command string, fd int) (bool, error) {
	client, err := Dial(host, port, user, password)
	if err != nil {
		return false, err
	}
	defer client.Close()

	stopKeepAlive := startKeepAlive(client)
	defer stopKeepAlive()

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

	// On Windows, term.GetSize calls GetConsoleScreenBufferInfo, which only
	// accepts a screen-buffer (stdout) handle — passing stdin's fd would fail.
	sizeFd := int(os.Stdout.Fd())
	width, height, err := term.GetSize(sizeFd)
	if err != nil {
		width, height = 80, 24
	}

	// Enable VT processing on the output handle so ANSI escapes from full-screen
	// programs (top, vim, htop) render correctly instead of as literal text.
	restoreVT, vtErr := enableVTOutput(windows.Handle(os.Stdout.Fd()))
	if vtErr == nil {
		defer restoreVT()
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

	// sigDone stops the handler goroutine when the session ends. signal.Stop only
	// halts delivery; it does not close sigCh, so without this the goroutine would
	// block on the channel forever and leak — one per session, accumulating across
	// every auto-reconnect.
	sigDone := make(chan struct{})
	defer close(sigDone)
	go func() {
		select {
		case <-sigDone:
			return
		case <-sigCh:
			// Treat a kill like an abrupt drop: restore the tty and clear any
			// alternate-screen / mouse / cursor-key modes a full-screen remote
			// program left set, so the shell we exit to is usable.
			term.Restore(fd, oldState)
			resetTerminalModes(true)
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
				w, h, err := term.GetSize(sizeFd)
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

	sessionDone := make(chan struct{})
	defer close(sessionDone)
	go broker.forward(stdin, sessionDone)

	if command == "" {
		if err := session.Shell(); err != nil {
			return true, fmt.Errorf("starting shell: %w", err)
		}
	} else {
		if err := session.Start(command); err != nil {
			return true, fmt.Errorf("starting command: %w", err)
		}
	}
	waitErr := session.Wait()
	// Clear any mouse-tracking / bracketed-paste modes a killed remote program
	// left behind, so the next session (or the local shell on exit) does not
	// receive mouse events as literal text. Only force-leave the alternate screen
	// when the session ended abnormally; on a clean exit the remote program
	// already did so, and doing it twice scrambles the restored screen.
	clean := waitErr == nil || isCleanExit(waitErr)
	resetTerminalModes(!clean)
	return true, waitErr
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

func enableVTOutput(h windows.Handle) (func(), error) {
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return nil, err
	}
	newMode := mode | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.DISABLE_NEWLINE_AUTO_RETURN
	if err := windows.SetConsoleMode(h, newMode); err != nil {
		return nil, err
	}
	return func() { windows.SetConsoleMode(h, mode) }, nil
}

func nextBackoff(current time.Duration) time.Duration {
	const maxBackoff = 30 * time.Second
	next := current * 2
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
