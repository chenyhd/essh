package prompt

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// SelectItem represents a selectable item.
type SelectItem struct {
	Label string
	Desc  string
}

// Select displays an interactive list and returns the selected index.
// Arrow keys and j/k to move, Enter to select, Ctrl+C or q to cancel.
func Select(label string, items []SelectItem, defaultIdx int) (int, error) {
	if len(items) == 0 {
		return -1, fmt.Errorf("no items to select")
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return -1, fmt.Errorf("not a terminal")
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return -1, fmt.Errorf("setting raw mode: %w", err)
	}
	defer term.Restore(fd, oldState)

	selected := defaultIdx
	if selected < 0 || selected >= len(items) {
		selected = 0
	}
	total := len(items)

	// Calculate label width for alignment
	labelW := 0
	for _, item := range items {
		if len(item.Label) > labelW {
			labelW = len(item.Label)
		}
	}

	// Print label and reserve space
	fmt.Fprintf(os.Stdout, "%s\r\n", label)
	for i := 0; i < total; i++ {
		fmt.Fprintf(os.Stdout, "\r\n")
	}

	render := func() {
		// Move cursor up to first item
		fmt.Fprintf(os.Stdout, "\033[%dA", total)
		for i, item := range items {
			prefix := "  "
			if i == selected {
				prefix = "> "
			}
			fmt.Fprintf(os.Stdout, "\033[2K\r  %s%-*s  %s\r\n", prefix, labelW, item.Label, item.Desc)
		}
	}

	render()

	readByte := func() (byte, error) {
		b := make([]byte, 1)
		_, err := os.Stdin.Read(b)
		return b[0], err
	}

	for {
		b, err := readByte()
		if err != nil {
			return -1, err
		}

		switch b {
		case '\r', '\n':
			return selected, nil
		case 3, 'q': // Ctrl+C or q
			return -1, fmt.Errorf("cancelled")
		case 'k':
			if selected > 0 {
				selected--
			}
		case 'j':
			if selected < total-1 {
				selected++
			}
		case '\x1b': // Escape sequence
			b2, err := readByte()
			if err != nil {
				return -1, err
			}
			if b2 == '[' {
				b3, err := readByte()
				if err != nil {
					return -1, err
				}
				switch b3 {
				case 'A': // up arrow
					if selected > 0 {
						selected--
					}
				case 'B': // down arrow
					if selected < total-1 {
						selected++
					}
				}
			}
		default:
			continue
		}

		render()
	}
}
