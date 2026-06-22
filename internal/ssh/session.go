package ssh

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

// Dial creates an SSH client connection with shared config (timeout, auth, host key).
func Dial(host string, port int, user, password string) (*ssh.Client, error) {
	config := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods(password),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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
