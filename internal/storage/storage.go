package storage

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"essh/internal/crypto"
)

// Server represents a saved SSH server entry.
type Server struct {
	Name              string `json:"name"`
	User              string `json:"user"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	EncryptedPassword string `json:"encrypted_password"`
}

// Store represents the essh-storage.json file.
type Store struct {
	Version      int      `json:"version"`
	Salt         string   `json:"salt"`
	Verification string   `json:"verification"`
	Servers      []Server `json:"servers"`
}

// Load reads the storage file from the given path.
func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading storage: %w", err)
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("parsing storage: %w", err)
	}
	return &store, nil
}

// Save writes the storage to the given path.
// Version is auto-incremented on each save.
func Save(path string, store *Store) error {
	store.Version++
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling storage: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// Init creates a new storage file with the given encryption password.
// If keyfile is provided, it is mixed into key derivation.
func Init(path string, encPassword string, keyfile []byte) error {
	salt, err := crypto.GenerateSalt()
	if err != nil {
		return err
	}
	key := crypto.DeriveKey(encPassword, salt, keyfile)
	verification, err := crypto.Encrypt(key, crypto.VerifyStr)
	if err != nil {
		return fmt.Errorf("encrypting verification: %w", err)
	}
	store := &Store{
		Salt:         hex.EncodeToString(salt),
		Verification: verification,
		Servers:      []Server{},
	}
	return Save(path, store)
}

// GetSalt returns the decoded salt from the store.
func (s *Store) GetSalt() ([]byte, error) {
	return hex.DecodeString(s.Salt)
}

// VerifyPassword checks if the encryption password is correct.
// If keyfile is provided, it is mixed into key derivation.
func (s *Store) VerifyPassword(encPassword string, keyfile []byte) ([]byte, error) {
	salt, err := s.GetSalt()
	if err != nil {
		return nil, fmt.Errorf("decoding salt: %w", err)
	}
	key := crypto.DeriveKey(encPassword, salt, keyfile)
	plaintext, err := crypto.Decrypt(key, s.Verification)
	if err != nil {
		return nil, fmt.Errorf("wrong encryption password")
	}
	if plaintext != crypto.VerifyStr {
		return nil, fmt.Errorf("wrong encryption password")
	}
	return key, nil
}

// FindServer returns a server by name, or nil if not found.
func (s *Store) FindServer(name string) *Server {
	for i := range s.Servers {
		if s.Servers[i].Name == name {
			return &s.Servers[i]
		}
	}
	return nil
}

// AddServer adds a new server entry to the store.
func (s *Store) AddServer(srv Server) error {
	if s.FindServer(srv.Name) != nil {
		return fmt.Errorf("server %q already exists", srv.Name)
	}
	s.Servers = append(s.Servers, srv)
	return nil
}

// RemoveServer removes a server by name.
func (s *Store) RemoveServer(name string) error {
	for i := range s.Servers {
		if s.Servers[i].Name == name {
			s.Servers = append(s.Servers[:i], s.Servers[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("server %q not found", name)
}

// RenameServer renames a server.
func (s *Store) RenameServer(oldName, newName string) error {
	if s.FindServer(newName) != nil {
		return fmt.Errorf("server %q already exists", newName)
	}
	srv := s.FindServer(oldName)
	if srv == nil {
		return fmt.Errorf("server %q not found", oldName)
	}
	srv.Name = newName
	return nil
}

// ReEncryptAll decrypts all passwords with oldKey and re-encrypts with newKey.
// Also updates the salt and verification string.
func (s *Store) ReEncryptAll(oldKey, newKey, newSalt []byte, newVerification string) error {
	for i := range s.Servers {
		plaintext, err := crypto.Decrypt(oldKey, s.Servers[i].EncryptedPassword)
		if err != nil {
			return fmt.Errorf("decrypting %q: %w", s.Servers[i].Name, err)
		}
		encrypted, err := crypto.Encrypt(newKey, plaintext)
		if err != nil {
			return fmt.Errorf("re-encrypting %q: %w", s.Servers[i].Name, err)
		}
		s.Servers[i].EncryptedPassword = encrypted
	}
	s.Salt = hex.EncodeToString(newSalt)
	s.Verification = newVerification
	return nil
}
