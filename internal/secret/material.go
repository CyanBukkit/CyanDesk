package secret

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

type Material struct {
	ServerID        string `json:"server_id"`
	SignPrivate     string `json:"sign_private"`
	SignPublic      string `json:"sign_public"`
	UpdatePrivate   string `json:"update_private"`
	UpdatePublic    string `json:"update_public"`
	TokenMAC        string `json:"token_mac"`
	InternalToken   string `json:"internal_token"`
	BootstrapHash   string `json:"bootstrap_hash,omitempty"`
	CertPEM         string `json:"cert_pem"`
	KeyPEM          string `json:"key_pem"`
	CertFingerprint string `json:"cert_fingerprint"`
	UpdateKeyID     string `json:"update_key_id"`
}

func Generate(hosts []string) (*Material, ed25519.PrivateKey, error) {
	serverID, err := RandomToken(16)
	if err != nil {
		return nil, nil, err
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, err
	}
	upub, upriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, err
	}
	mac := make([]byte, 32)
	if _, err := rand.Read(mac); err != nil {
		return nil, nil, err
	}
	internal, err := RandomToken(32)
	if err != nil {
		return nil, nil, err
	}
	certPEM, keyPEM, fingerprint, err := mintCert(hosts)
	if err != nil {
		return nil, nil, err
	}
	m := &Material{
		ServerID:        serverID,
		SignPrivate:     base64.RawStdEncoding.EncodeToString(priv),
		SignPublic:      base64.RawStdEncoding.EncodeToString(pub),
		UpdatePrivate:   base64.RawStdEncoding.EncodeToString(upriv),
		UpdatePublic:    base64.RawStdEncoding.EncodeToString(upub),
		TokenMAC:        base64.RawStdEncoding.EncodeToString(mac),
		InternalToken:   internal,
		CertPEM:         string(certPEM),
		KeyPEM:          string(keyPEM),
		CertFingerprint: fingerprint,
		UpdateKeyID:     "update-1",
	}
	return m, priv, nil
}

func (m *Material) SignKey() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	priv, err := base64.RawStdEncoding.DecodeString(m.SignPrivate)
	if err != nil {
		return nil, nil, err
	}
	pub, err := base64.RawStdEncoding.DecodeString(m.SignPublic)
	if err != nil {
		return nil, nil, err
	}
	return ed25519.PrivateKey(priv), ed25519.PublicKey(pub), nil
}

func (m *Material) UpdateKey() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	priv, err := base64.RawStdEncoding.DecodeString(m.UpdatePrivate)
	if err != nil {
		return nil, nil, err
	}
	pub, err := base64.RawStdEncoding.DecodeString(m.UpdatePublic)
	if err != nil {
		return nil, nil, err
	}
	return ed25519.PrivateKey(priv), ed25519.PublicKey(pub), nil
}

func (m *Material) MAC() ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(m.TokenMAC)
}

func Save(path string, m *Material) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return os.WriteFile(path, buf, 0o600)
}

func Load(path string) (*Material, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Material
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func mintCert(hosts []string) ([]byte, []byte, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "cyandesk"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(825 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if h != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	if len(tmpl.DNSNames) == 0 && len(tmpl.IPAddresses) == 0 {
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, "", err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	sum := sha256.Sum256(der)
	return certPEM, keyPEM, hex.EncodeToString(sum[:]), nil
}

func FingerprintFromPEM(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", errorsNew("no cert")
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

func errorsNew(s string) error { return simpleError(s) }
