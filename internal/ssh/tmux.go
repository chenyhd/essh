package ssh

import (
	"bytes"
	"sort"
	"strings"
)

// TmuxSession describes a tmux session running on the remote host.
type TmuxSession struct {
	Name     string
	Windows  string
	Attached bool
}

// tmuxSep delimits the fields of each tmux -F record. It must be a *printable*
// character: tmux escapes control bytes in format output (a literal 0x1F came
// back as the four-character text "\037"), which broke field splitting. The
// variable-length session name is emitted last so that, even if a name itself
// contains the delimiter, SplitN(line, tmuxSep, 3) keeps it intact in the final
// field. The two fixed fields before it — window count (digits) and the attached
// marker — never contain "|".
const tmuxSep = "|"

// tmuxAbsentSentinel is printed by the remote probe when tmux is not installed,
// letting us tell "tmux missing" (skip the menu, open a plain shell) apart from
// "tmux present but no sessions" (still offer to create one).
const tmuxAbsentSentinel = "essh-tmux-absent"

// ListTmuxSessions returns the tmux sessions on the remote host. The available
// return reports whether tmux is installed there: when false the caller should
// connect with a plain shell instead of offering a session menu. A nil error
// with available=true and an empty slice means tmux is installed but has no
// sessions yet. A non-nil error means the SSH connection itself failed.
//
// The probe and listing run through a login shell (see loginShell) so they see
// the same PATH an interactive login does; otherwise tmux installed under a
// non-default prefix (Homebrew, /usr/local, ~/bin) would look absent.
func ListTmuxSessions(host string, port int, user, password string) (sessions []TmuxSession, available bool, err error) {
	client, err := Dial(host, port, user, password)
	if err != nil {
		return nil, false, err
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return nil, false, err
	}
	defer sess.Close()

	format := "#{session_windows}" + tmuxSep + "#{?session_attached,attached,}" + tmuxSep + "#{session_name}"
	listCmd := "if command -v tmux >/dev/null 2>&1; then tmux list-sessions -F '" + format + "'; else printf '%s\\n' '" + tmuxAbsentSentinel + "'; fi"

	var stdout bytes.Buffer
	sess.Stdout = &stdout
	// tmux writes "no server running" to stderr and exits non-zero when there are
	// no sessions; that is not an error here, so stderr and the run error are
	// ignored and we read the result purely from stdout.
	sess.Stderr = nil
	_ = sess.Run(loginShell(listCmd))

	text := stdout.String()
	if strings.Contains(text, tmuxAbsentSentinel) {
		return nil, false, nil
	}

	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		// Only lines carrying the field separator are session records; a login
		// shell may emit unrelated profile chatter.
		parts := strings.SplitN(line, tmuxSep, 3)
		if len(parts) < 3 {
			continue
		}
		sessions = append(sessions, TmuxSession{
			Windows:  parts[0],
			Attached: parts[1] == "attached",
			Name:     parts[2],
		})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Name < sessions[j].Name })
	return sessions, true, nil
}

// TmuxAttachCommand builds an idempotent attach-or-create command for a named
// session. new-session -A attaches when the session exists and creates it
// otherwise, which keeps auto-reconnect seamless: re-running it after a dropped
// link reattaches to the same still-alive session rather than spawning a new one.
// It runs through a login shell for the same PATH reason as ListTmuxSessions, and
// execs tmux so the session ends exactly when tmux does.
func TmuxAttachCommand(name string) string {
	return loginShell("exec tmux new-session -A -s " + shellQuote(name))
}

// loginShell wraps cmd so the remote runs it under the user's login shell with
// login initialization (PATH, etc.). ${SHELL:-/bin/sh} falls back to /bin/sh if
// sshd did not export SHELL.
func loginShell(cmd string) string {
	return "${SHELL:-/bin/sh} -lc " + shellQuote(cmd)
}

// shellQuote single-quote-escapes s so it is safe as an argument in the remote
// command line that SSH runs for a non-shell session.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
