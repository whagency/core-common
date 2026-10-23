package cryptox

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNewAESGCMFromBase64_RejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{
			name:    "invalid base64",
			key:     "%%%not-base64%%%",
			wantErr: "decode key",
		},
		{
			name:    "invalid key size",
			key:     base64.StdEncoding.EncodeToString([]byte("short-key")),
			wantErr: "invalid key size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cipher, err := NewAESGCMFromBase64(tt.key)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if cipher != nil {
				t.Fatal("expected nil cipher on invalid input")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error to contain %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestAESGCM_EncryptDecryptRoundTrip(t *testing.T) {
	cipher, err := NewAESGCMFromBase64(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef")))
	if err != nil {
		t.Fatalf("NewAESGCMFromBase64() error = %v", err)
	}

	plaintext := []byte(`{"account_id":"12345","amount":"100.50"}`)

	ciphertext, err := cipher.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if len(ciphertext) <= len(plaintext) {
		t.Fatalf("expected nonce-prefixed ciphertext to be larger than plaintext")
	}
	if string(ciphertext) == string(plaintext) {
		t.Fatal("expected ciphertext to differ from plaintext")
	}

	decrypted, err := cipher.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("Decrypt() = %q, want %q", decrypted, plaintext)
	}
}

func TestAESGCM_DecryptWithWrongKeyFails(t *testing.T) {
	cipherA, err := NewAESGCMFromBase64(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef")))
	if err != nil {
		t.Fatalf("NewAESGCMFromBase64() error = %v", err)
	}
	cipherB, err := NewAESGCMFromBase64(base64.StdEncoding.EncodeToString([]byte("fedcba9876543210")))
	if err != nil {
		t.Fatalf("NewAESGCMFromBase64() error = %v", err)
	}

	ciphertext, err := cipherA.Encrypt([]byte("secret payload"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	plaintext, err := cipherB.Decrypt(ciphertext)
	if err == nil {
		t.Fatalf("expected decrypt error, got plaintext %q", plaintext)
	}
	if !strings.Contains(err.Error(), "open") {
		t.Fatalf("expected decrypt error to contain %q, got %q", "open", err.Error())
	}
}

func TestAESGCM_DecryptCorruptedCiphertextFails(t *testing.T) {
	cipher, err := NewAESGCMFromBase64(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef")))
	if err != nil {
		t.Fatalf("NewAESGCMFromBase64() error = %v", err)
	}

	ciphertext, err := cipher.Encrypt([]byte("secret payload"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	corrupted := append([]byte(nil), ciphertext...)
	corrupted[len(corrupted)-1] ^= 0xFF

	plaintext, err := cipher.Decrypt(corrupted)
	if err == nil {
		t.Fatalf("expected decrypt error, got plaintext %q", plaintext)
	}
	if !strings.Contains(err.Error(), "open") {
		t.Fatalf("expected decrypt error to contain %q, got %q", "open", err.Error())
	}
}
