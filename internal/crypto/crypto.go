// Package crypto implements the PII-protection primitives used across the
// service:
//
//   - Cipher: authenticated symmetric encryption (AES-256-GCM) for PII at rest.
//     Every applicant's phone, email, address, etc. is encrypted with a
//     per-record random nonce and stored as ciphertext. Losing the key means
//     the data is unrecoverable; leaking the database without the key reveals
//     nothing.
//
//   - BlindIndex: a keyed HMAC-SHA256 fingerprint of a normalized value. It lets
//     us look up an applicant by phone number WITHOUT storing the phone in
//     plaintext or being able to reverse the index. Equal inputs produce equal
//     indexes (so we can query), but the index reveals nothing about the input
//     to anyone without the key.
//
//   - Signer: keyed HMAC used to sign verification codes / QR payloads on admit
//     cards so their authenticity can be checked later.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ---- Cipher: AES-256-GCM ----------------------------------------------------

type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds an AES-256-GCM cipher from a 32-byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("crypto: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt seals plaintext and returns base64(nonce || ciphertext || tag).
// Empty input returns an empty string (so optional fields stay empty, not a
// blob of encrypted emptiness).
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. An empty string decrypts to empty.
func (c *Cipher) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("crypto: invalid base64: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(sealed) < ns {
		return "", errors.New("crypto: ciphertext too short")
	}
	nonce, ct := sealed[:ns], sealed[ns:]
	plaintext, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", errors.New("crypto: authentication failed")
	}
	return string(plaintext), nil
}

// ---- BlindIndex: keyed HMAC fingerprint -------------------------------------

type BlindIndex struct {
	key []byte
}

func NewBlindIndex(key []byte) *BlindIndex {
	return &BlindIndex{key: key}
}

// Hash returns the hex HMAC-SHA256 of the normalized value. Used as a lookup
// column: deterministic, non-reversible, keyed.
func (b *BlindIndex) Hash(value string) string {
	norm := normalize(value)
	mac := hmac.New(sha256.New, b.key)
	mac.Write([]byte(norm))
	return hex.EncodeToString(mac.Sum(nil))
}

// normalize collapses trivial variations so lookups are robust: trims, lowercases,
// and strips spaces/dashes/parens common in phone-number formatting.
func normalize(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	replacer := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", "+", "")
	return replacer.Replace(v)
}

// ---- Signer: HMAC signatures for verification payloads ----------------------

type Signer struct {
	key []byte
}

func NewSigner(key []byte) *Signer {
	return &Signer{key: key}
}

// Sign returns a short hex signature (first 16 bytes of HMAC-SHA256) for msg.
func (s *Signer) Sign(msg string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// Verify checks a signature in constant time.
func (s *Signer) Verify(msg, sig string) bool {
	expected := s.Sign(msg)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(sig)) == 1
}
