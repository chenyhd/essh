package crypto

import (
	"crypto/rand"
	"fmt"
	"os"
)

const KeyfileLen = 32

// GenerateKeyfile writes 32 cryptographically random bytes to the given path.
func GenerateKeyfile(path string) error {
	data := make([]byte, KeyfileLen)
	if _, err := rand.Read(data); err != nil {
		return fmt.Errorf("generating keyfile: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// LoadKeyfile reads keyfile contents from the given path.
func LoadKeyfile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading keyfile: %w", err)
	}
	return data, nil
}
