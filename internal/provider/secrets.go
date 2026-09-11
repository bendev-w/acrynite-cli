package provider

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"

	"acrynite-cli/utils"
)

// SecretStore encrypts provider secrets (tokens, access keys) at rest using
// AES-GCM with a machine-local key file. This is a fallback used because no
// OS keychain integration is wired up yet - it is NOT a substitute for a real
// keychain and should be replaced with one (e.g. go-keyring) in a future phase.
type SecretStore struct {
	keyPath    string
	secretPath string
	mu         sync.Mutex
}

const secretKeySize = 32 // AES-256

// NewSecretStore creates (or opens) the local secret store under ~/.acrynite-cli/.
func NewSecretStore() (*SecretStore, error) {
	home, err := utils.GetHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	dir := filepath.Join(home, ".acrynite-cli")
	if err := utils.EnsureDirExists(dir); err != nil {
		return nil, fmt.Errorf("failed to create secret store directory: %w", err)
	}

	s := &SecretStore{
		keyPath:    filepath.Join(dir, ".secret.key"),
		secretPath: filepath.Join(dir, "secrets.enc.json"),
	}

	if !utils.Exists(s.keyPath) {
		key := make([]byte, secretKeySize)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("failed to generate secret key: %w", err)
		}
		if err := utils.WriteFile(s.keyPath, []byte(base64.StdEncoding.EncodeToString(key))); err != nil {
			return nil, fmt.Errorf("failed to write secret key: %w", err)
		}
	}

	return s, nil
}

func (s *SecretStore) loadKey() ([]byte, error) {
	data, err := utils.ReadFile(s.keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read secret key: %w", err)
	}
	return base64.StdEncoding.DecodeString(string(data))
}

func (s *SecretStore) loadAll() (map[string]string, error) {
	secrets := make(map[string]string)
	if !utils.Exists(s.secretPath) {
		return secrets, nil
	}
	data, err := utils.ReadFile(s.secretPath)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &secrets); err != nil {
		return nil, err
	}
	return secrets, nil
}

func (s *SecretStore) saveAll(secrets map[string]string) error {
	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return err
	}
	return utils.WriteFile(s.secretPath, data)
}

func (s *SecretStore) newCipher() (cipher.AEAD, error) {
	key, err := s.loadKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Set encrypts and stores a secret value under the given key (e.g. provider name).
func (s *SecretStore) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	gcm, err := s.newCipher()
	if err != nil {
		return fmt.Errorf("failed to initialize encryption: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(value), nil)

	secrets, err := s.loadAll()
	if err != nil {
		return fmt.Errorf("failed to load secret store: %w", err)
	}
	secrets[key] = base64.StdEncoding.EncodeToString(ciphertext)

	return s.saveAll(secrets)
}

// Get decrypts and returns the secret stored under key.
func (s *SecretStore) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	secrets, err := s.loadAll()
	if err != nil {
		return "", fmt.Errorf("failed to load secret store: %w", err)
	}

	encoded, exists := secrets[key]
	if !exists {
		return "", fmt.Errorf("no secret stored for '%s'", key)
	}

	gcm, err := s.newCipher()
	if err != nil {
		return "", fmt.Errorf("failed to initialize encryption: %w", err)
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode secret: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("corrupted secret data for '%s'", key)
	}

	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt secret (key file may be missing or corrupted): %w", err)
	}

	return string(plaintext), nil
}

// Delete removes a stored secret.
func (s *SecretStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	secrets, err := s.loadAll()
	if err != nil {
		return fmt.Errorf("failed to load secret store: %w", err)
	}
	delete(secrets, key)
	return s.saveAll(secrets)
}
