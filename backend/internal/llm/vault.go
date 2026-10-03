// Package llm is the LLM gateway: the per-teacher key vault, provider
// adapters, pseudonymised prompts and the marking jobs (FR-EV-02/03/05,
// BR-10, ADR-06, ADR-09, ADR-16).
package llm

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// Vault wraps per-record data keys (DEKs) with the master key (KEK).
type Vault struct {
	kek   []byte
	kekID string
}

// NewVault builds a vault from a 32-byte key.
func NewVault(kek []byte) (*Vault, error) {
	if len(kek) != 32 {
		return nil, errors.New("the master key must be 32 bytes")
	}
	sum := sha256.Sum256(kek)
	return &Vault{kek: append([]byte(nil), kek...), kekID: hex.EncodeToString(sum[:4])}, nil
}

// LoadKEK reads the master key from a file, never from an environment
// variable (ADR-09). The file may hold 32 raw bytes, 64 hex characters or
// standard base64. On Unix the file must not be readable by group or others.
func LoadKEK(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("master key file: %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("master key file %s must not be accessible to group/others (chmod 0400)", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) == 32 {
		return raw, nil
	}
	s := strings.TrimSpace(string(raw))
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	return nil, errors.New("master key file must contain 32 raw bytes, 64 hex characters or base64 of 32 bytes")
}

// Sealed is what the database stores for one secret.
type Sealed struct {
	Ciphertext, Nonce, WrappedDEK, DEKNonce []byte
	KEKID                                   string
}

func gcm(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func random(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// aad binds a ciphertext to its owner, provider and version so a stored key
// cannot be swapped onto another teacher's record (ADR-09).
func aad(teacherID, provider string, version int) []byte {
	return []byte(fmt.Sprintf("%s|%s|%d", teacherID, provider, version))
}

// Seal encrypts plaintext with a fresh DEK and wraps the DEK with the KEK.
func (v *Vault) Seal(plaintext []byte, teacherID, provider string, version int) (Sealed, error) {
	dek := random(32)
	defer zero(dek)
	data, err := gcm(dek)
	if err != nil {
		return Sealed{}, err
	}
	s := Sealed{Nonce: random(12), DEKNonce: random(12), KEKID: v.kekID}
	s.Ciphertext = data.Seal(nil, s.Nonce, plaintext, aad(teacherID, provider, version))
	wrap, err := gcm(v.kek)
	if err != nil {
		return Sealed{}, err
	}
	s.WrappedDEK = wrap.Seal(nil, s.DEKNonce, dek, []byte(v.kekID))
	return s, nil
}

// Open decrypts a sealed secret. Callers must zero the result after use.
func (v *Vault) Open(s Sealed, teacherID, provider string, version int) ([]byte, error) {
	if s.KEKID != v.kekID {
		return nil, fmt.Errorf("key was sealed with master key %s, not the current %s", s.KEKID, v.kekID)
	}
	wrap, err := gcm(v.kek)
	if err != nil {
		return nil, err
	}
	dek, err := wrap.Open(nil, s.DEKNonce, s.WrappedDEK, []byte(v.kekID))
	if err != nil {
		return nil, errors.New("could not unwrap the data key")
	}
	defer zero(dek)
	data, err := gcm(dek)
	if err != nil {
		return nil, err
	}
	pt, err := data.Open(nil, s.Nonce, s.Ciphertext, aad(teacherID, provider, version))
	if err != nil {
		return nil, errors.New("could not decrypt the stored key")
	}
	return pt, nil
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
