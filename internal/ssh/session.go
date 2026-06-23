package ssh

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
)

// Dial creates an SSH client connection with shared config (timeout, auth, host key).
func Dial(host string, port int, user, password string) (*ssh.Client, error) {
	hostKey, err := hostKeyCallback()
	if err != nil {
		return nil, fmt.Errorf("loading known_hosts: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods(password),
		HostKeyCallback: hostKey,
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}
	return client, nil
}

const (
	keepAliveInterval = 15 * time.Second
	keepAliveTimeout  = 10 * time.Second
	keepAliveMaxFails = 3
)

// startKeepAlive periodically pings the server so that a silently-dropped link
// (pulled cable, dead WiFi, host crash) is detected without waiting on the OS
// TCP timeout or on a failed user keystroke. After keepAliveMaxFails consecutive
// missed pings it closes the client, which unblocks session.Wait() so the caller
// can run its reconnect logic promptly. The returned stop function must be called
// when the session ends normally to shut the goroutine down.
func startKeepAlive(client *ssh.Client) func() {
	done := make(chan struct{})
	go func() {
		fails := 0
		for {
			select {
			case <-done:
				return
			case <-time.After(keepAliveInterval):
			}
			if keepAlivePing(client, keepAliveTimeout) != nil {
				fails++
				if fails >= keepAliveMaxFails {
					client.Close()
					return
				}
				// Link is suspect; retry immediately rather than waiting a
				// full interval so the disconnect surfaces quickly.
				continue
			}
			fails = 0
		}
	}()
	return func() { close(done) }
}

// keepAlivePing sends a single keepalive request and returns an error if the
// server does not reply within timeout. The request runs in its own goroutine
// because SendRequest can block indefinitely on a dead connection.
func keepAlivePing(client *ssh.Client, timeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() {
		_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
		errCh <- err
	}()
	select {
	case err := <-errCh:
		return err
	case <-time.After(timeout):
		return errors.New("keepalive timeout")
	}
}

// resetTerminalModes returns the local terminal emulator to a sane state after a
// session ends. When a connection drops abruptly, the remote shell (and whatever
// full-screen program it was running — vim, tmux, htop, less) is killed without
// getting to emit its usual teardown sequences, so the emulator is left with
// mouse tracking, bracketed paste, or the alternate screen still enabled. Those
// are properties of the terminal emulator, not the local tty, so term.Restore
// does not clear them: the next shell prompt then receives mouse motion as
// literal text like "35;79;46M". Emitting the disable sequences ourselves
// restores normal input. The sequences are no-ops when the modes are already off,
// so running this after a clean exit is harmless.
func resetTerminalModes() {
	const reset = "\x1b[?1000l" + // disable X10/normal mouse tracking
		"\x1b[?1002l" + // disable button-event mouse tracking
		"\x1b[?1003l" + // disable any-event mouse tracking
		"\x1b[?1006l" + // disable SGR mouse encoding
		"\x1b[?1015l" + // disable urxvt mouse encoding
		"\x1b[?2004l" + // disable bracketed paste
		"\x1b[?1049l" + // leave the alternate screen buffer
		"\x1b[?25h" + // show the cursor
		"\x1b[?7h" // re-enable line wrap
	os.Stdout.WriteString(reset)
}

// stdinBroker owns os.Stdin for the entire Connect lifetime and hands out the
// bytes it reads over a single channel. This guarantees there is never more
// than one consumer of the keyboard at a time: a per-session forwarder while a
// shell is live, or waitForEnter while reconnect is paused. Reads from os.Stdin
// cannot be cancelled, so the previous design — one io.Copy(os.Stdin) goroutine
// per session — leaked a stale reader on every drop. Those zombies then competed
// with the next session (and with the Enter prompt) for input, silently eating
// the first line typed after a reconnect.
type stdinBroker struct {
	ch chan []byte
}

func newStdinBroker() *stdinBroker {
	b := &stdinBroker{ch: make(chan []byte)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				b.ch <- chunk
			}
			if err != nil {
				close(b.ch)
				return
			}
		}
	}()
	return b
}

// forward pumps keyboard input into the live session's stdin until the session
// ends (done is closed), the pipe errors, or stdin reaches EOF. When forward
// returns, no goroutine is left holding the keyboard, so the next consumer
// receives all subsequent input.
func (b *stdinBroker) forward(stdin io.WriteCloser, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case chunk, ok := <-b.ch:
			if !ok {
				stdin.Close()
				return
			}
			if _, err := stdin.Write(chunk); err != nil {
				return
			}
		}
	}
}

// waitForLine blocks until the user presses Enter (a chunk containing CR or LF),
// returning io.EOF if stdin closes first.
func (b *stdinBroker) waitForLine() error {
	for {
		chunk, ok := <-b.ch
		if !ok {
			return io.EOF
		}
		if bytes.ContainsAny(chunk, "\r\n") {
			return nil
		}
	}
}

// sleepOrEnter waits up to d, returning true if the user pressed Enter (a chunk
// containing CR or LF) to skip the remaining wait. Any other input received
// during the wait is discarded. Because the broker is the sole reader of
// os.Stdin, this needs no read deadlines and never races a session's input pump.
func (b *stdinBroker) sleepOrEnter(d time.Duration) bool {
	if d <= 0 {
		return false
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return false
		case chunk, ok := <-b.ch:
			if !ok {
				<-timer.C
				return false
			}
			if bytes.ContainsAny(chunk, "\r\n") {
				return true
			}
		}
	}
}
