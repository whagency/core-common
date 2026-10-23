package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

// AESGCM provides symmetric encryption/decryption backed by AES-GCM.
// Ciphertext format is: nonce || encrypted_payload.
type AESGCM struct {
	aead      cipher.AEAD
	nonceSize int
}

// NewAESGCMFromBase64 builds AES-GCM helper from base64-encoded raw key bytes.
func NewAESGCMFromBase64(masterKeyBase64 string) (*AESGCM, error) {
	masterKeyBase64 = strings.TrimSpace(masterKeyBase64)
	if masterKeyBase64 == "" {
		return nil, fmt.Errorf("[cryptox:NewAESGCMFromBase64][err: key is empty]")
	}

	rawKey, err := base64.StdEncoding.DecodeString(masterKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("[cryptox:NewAESGCMFromBase64][err: decode key: %w]", err)
	}

	switch len(rawKey) {
	case 16, 24, 32:
	default:
		return nil, fmt.Errorf("[cryptox:NewAESGCMFromBase64][err: invalid key size: %d]", len(rawKey))
	}

	block, err := aes.NewCipher(rawKey)
	if err != nil {
		return nil, fmt.Errorf("[cryptox:NewAESGCMFromBase64][err: new cipher: %w]", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("[cryptox:NewAESGCMFromBase64][err: new gcm: %w]", err)
	}

	return &AESGCM{
		aead:      aead,
		nonceSize: aead.NonceSize(),
	}, nil
}

// Encrypt encrypts plaintext and returns nonce-prefixed ciphertext bytes.
func (a *AESGCM) Encrypt(plaintext []byte) ([]byte, error) {
	if a == nil || a.aead == nil {
		return nil, fmt.Errorf("[cryptox:AESGCM:Encrypt][err: cipher is not initialized]")
	}

	out := make([]byte, a.nonceSize, a.nonceSize+len(plaintext)+a.aead.Overhead())
	nonce := out[:a.nonceSize]

	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("[cryptox:AESGCM:Encrypt][err: nonce: %w]", err)
	}

	out = a.aead.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// Decrypt decrypts nonce-prefixed ciphertext bytes.
func (a *AESGCM) Decrypt(ciphertext []byte) ([]byte, error) {
	if a == nil || a.aead == nil {
		return nil, fmt.Errorf("[cryptox:AESGCM:Decrypt][err: cipher is not initialized]")
	}

	if len(ciphertext) < a.nonceSize {
		return nil, fmt.Errorf("[cryptox:AESGCM:Decrypt][err: ciphertext too short]")
	}

	nonce := ciphertext[:a.nonceSize]
	payload := ciphertext[a.nonceSize:]

	plaintext, err := a.aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return nil, fmt.Errorf("[cryptox:AESGCM:Decrypt][err: open: %w]", err)
	}

	return plaintext, nil
}
