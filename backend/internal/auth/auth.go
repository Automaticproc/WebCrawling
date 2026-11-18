package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const SessionTTL = 7 * 24 * time.Hour

var ErrSession = errors.New("登录已失效")

func Hash(password string) (string, error) {
	sum, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(sum), nil
}

func Check(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func Sign(secret, username string, exp time.Time) (string, error) {
	if strings.Contains(username, "|") || username == "" {
		return "", errors.New("用户名无效")
	}
	payload := username + "|" + strconv.FormatInt(exp.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	token := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + hex.EncodeToString(mac.Sum(nil))
	return token, nil
}

func Verify(secret, token string, now time.Time) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", ErrSession
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", ErrSession
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	want := hex.EncodeToString(mac.Sum(nil))
	got, err := hex.DecodeString(parts[1])
	wantBytes, err2 := hex.DecodeString(want)
	if err != nil || err2 != nil || !hmac.Equal(got, wantBytes) {
		return "", ErrSession
	}
	name, expRaw, ok := strings.Cut(string(payload), "|")
	if !ok || name == "" {
		return "", ErrSession
	}
	expUnix, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil || !now.Before(time.Unix(expUnix, 0)) {
		return "", ErrSession
	}
	return name, nil
}

func RandomSecret() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
