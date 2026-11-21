// Package secretbox encrypts small secrets (platform login passwords) at rest.
// The key is derived from SESSION_SECRET, so rotating that secret invalidates
// stored passwords and they must be entered again.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const prefix = "v1:"

func key(secret string) []byte {
	sum := sha256.Sum256([]byte("catalog-platform-login:v1:" + secret))
	return sum[:]
}

// Encrypt returns an empty string for empty input.
func Encrypt(secret, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	block, err := aes.NewCipher(key(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.RawURLEncoding.EncodeToString(out), nil
}

// Decrypt accepts a token produced by Encrypt. Empty input returns empty.
func Decrypt(secret, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	if !strings.HasPrefix(token, prefix) {
		return "", errors.New("密码密文格式无效")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, prefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key(secret))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("密码密文长度无效")
	}
	nonce, body := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
