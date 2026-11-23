package identity

import (
	"crypto/sha256"
	"encoding/hex"
)

func EnrollMessage(nonce, pub, noise, name string) []byte {
	return []byte("enroll\n" + nonce + "\n" + pub + "\n" + noise + "\n" + name + "\n")
}

func ProofMessage(ts, nonce, method, path string, body []byte) []byte {
	sum := sha256.Sum256(body)
	return []byte("proof\n" + ts + "\n" + nonce + "\n" + method + "\n" + path + "\n" + hex.EncodeToString(sum[:]) + "\n")
}

func Fingerprint(pub, noise []byte) string {
	sum := sha256.Sum256(append(append([]byte{}, pub...), noise...))
	return hex.EncodeToString(sum[:])
}
