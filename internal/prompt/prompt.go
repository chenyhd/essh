package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadLine reads a line of input with a prompt message.
func ReadLine(prompt string) (string, error) {
	fmt.Print(prompt)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no input")
	}
	return strings.TrimSpace(scanner.Text()), nil
}

// ReadPassword reads a password without echoing it to the terminal.
func ReadPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	return string(pw), nil
}

// ReadPasswordConfirm reads a password twice and ensures they match.
func ReadPasswordConfirm(prompt, confirmPrompt string) (string, error) {
	pw, err := ReadPassword(prompt)
	if err != nil {
		return "", err
	}
	pw2, err := ReadPassword(confirmPrompt)
	if err != nil {
		return "", err
	}
	if pw != pw2 {
		return "", fmt.Errorf("passwords do not match")
	}
	return pw, nil
}
