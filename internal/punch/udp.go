package punch

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net"
	"time"
)

const packetLen = 64

var ErrCookie = errors.New("punch: cookie rejected")

// CookieRequest builds a 64-byte probe. The reply is the same size.
func CookieRequest(nonce []byte) []byte {
	buf := make([]byte, packetLen)
	buf[0] = 1
	copy(buf[1:], nonce)
	return buf
}

func CookieReply(nonce, cookie []byte) []byte {
	buf := make([]byte, packetLen)
	buf[0] = 2
	copy(buf[1:33], nonce)
	copy(buf[33:], cookie)
	return buf
}

func MakeCookie(secret, nonce []byte, addr *net.UDPAddr) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(nonce)
	_, _ = mac.Write([]byte(addr.String()))
	return mac.Sum(nil)[:31]
}

func CheckCookie(secret, nonce, cookie []byte, addr *net.UDPAddr) bool {
	want := MakeCookie(secret, nonce, addr)
	return hmac.Equal(want, cookie)
}

// Exchange performs an equal-sized cookie round trip and returns the RTT.
func Exchange(conn *net.UDPConn, peer *net.UDPAddr, secret []byte) (time.Duration, error) {
	var nonce [32]byte
	nonce[0] = byte(time.Now().UnixNano())
	nonce[1] = byte(len(secret))
	start := time.Now()
	if _, err := conn.WriteToUDP(CookieRequest(nonce[:]), peer); err != nil {
		return 0, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, packetLen)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return 0, err
	}
	if n != packetLen || buf[0] != 2 || !hmac.Equal(buf[1:33], nonce[:]) {
		return 0, ErrCookie
	}
	return time.Since(start), nil
}
