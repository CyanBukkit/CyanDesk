package secret

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

type AccessClaims struct {
	Subject string `json:"sub"`
	Role    string `json:"role"`
	Epoch   int64  `json:"epoch"`
	Issued  int64  `json:"iat"`
	Expires int64  `json:"exp"`
	JTI     string `json:"jti"`
}

func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func SignAccess(macKey []byte, c AccessClaims) (string, error) {
	body, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, macKey)
	_, _ = mac.Write(body)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func ParseAccess(macKey []byte, token string, now time.Time) (AccessClaims, error) {
	var zero AccessClaims
	i := -1
	for n := 0; n < len(token); n++ {
		if token[n] == '.' {
			i = n
			break
		}
	}
	if i <= 0 || i == len(token)-1 {
		return zero, errors.New("token: malformed")
	}
	body, err := base64.RawURLEncoding.DecodeString(token[:i])
	if err != nil {
		return zero, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(token[i+1:])
	if err != nil {
		return zero, err
	}
	mac := hmac.New(sha256.New, macKey)
	_, _ = mac.Write(body)
	if subtle.ConstantTimeCompare(sig, mac.Sum(nil)) != 1 {
		return zero, errors.New("token: bad signature")
	}
	var c AccessClaims
	if err := json.Unmarshal(body, &c); err != nil {
		return zero, err
	}
	if now.Unix() >= c.Expires {
		return zero, errors.New("token: expired")
	}
	return c, nil
}
