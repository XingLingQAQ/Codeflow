//go:build !windows

package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const secretMasterKeyEnv = "CODEFLOW_SECRET_MASTER_KEY"

func secretProtectorAvailable() error {
	_, err := loadSecretMasterKey()
	return err
}

func protectSecret(plaintext []byte) ([]byte, error) {
	gcm, err := secretGCM()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func unprotectSecret(ciphertext []byte) ([]byte, error) {
	gcm, err := secretGCM()
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext is invalid")
	}
	return gcm.Open(nil, ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():], nil)
}

func secretGCM() (cipher.AEAD, error) {
	key, err := loadSecretMasterKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func loadSecretMasterKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(secretMasterKeyEnv))
	if raw == "" {
		return nil, fmt.Errorf("%s must contain a base64-encoded 32-byte key", secretMasterKeyEnv)
	}
	key, err := base64.RawStdEncoding.DecodeString(raw)
	if err != nil {
		key, err = base64.StdEncoding.DecodeString(raw)
	}
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("%s must contain a base64-encoded 32-byte key", secretMasterKeyEnv)
	}
	return key, nil
}
