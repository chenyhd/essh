package ssh

import (
	"errors"
	"os"
	"time"
)

// sleepOrEnter waits up to d, returning true if the user pressed Enter to
// skip the wait. Any other input typed during the wait is discarded. If the
// platform doesn't support read deadlines on stdin, it falls back to a plain
// time.Sleep and always returns false.
func sleepOrEnter(d time.Duration) bool {
	if d <= 0 {
		return false
	}
	deadline := time.Now().Add(d)
	if err := os.Stdin.SetReadDeadline(deadline); err != nil {
		time.Sleep(d)
		return false
	}
	defer os.Stdin.SetReadDeadline(time.Time{})

	buf := make([]byte, 256)
	for {
		n, err := os.Stdin.Read(buf)
		for i := 0; i < n; i++ {
			if buf[i] == '\n' || buf[i] == '\r' {
				return true
			}
		}
		if err != nil {
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				if rem := time.Until(deadline); rem > 0 {
					time.Sleep(rem)
				}
			}
			return false
		}
	}
}

// interruptStdinReader wakes any goroutine currently blocked reading os.Stdin
// so it can exit before another reader starts. Safe to call even when read
// deadlines aren't supported — in that case it's a no-op.
func interruptStdinReader(done <-chan struct{}) {
	if err := os.Stdin.SetReadDeadline(time.Now()); err != nil {
		return
	}
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
	}
	os.Stdin.SetReadDeadline(time.Time{})
}
