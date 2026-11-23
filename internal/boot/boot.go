package boot

import (
	"os"
	"path/filepath"

	"cyandesk/internal/secret"
	"cyandesk/internal/store"
)

// Env is the on-disk server state. Token files are mode 0600 and are not logged.
type Env struct {
	Dir           string
	Material      *secret.Material
	DB            *store.Store
	Artifacts     string
	BootstrapFile string
	InternalFile  string
	CertFile      string
	KeyFile       string
}

func Open(dir string, hosts []string) (*Env, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	secretsPath := filepath.Join(dir, "secrets.json")
	var mat *secret.Material
	var err error
	if _, statErr := os.Stat(secretsPath); statErr == nil {
		mat, err = secret.Load(secretsPath)
	} else {
		mat, _, err = secret.Generate(hosts)
		if err == nil {
			err = secret.Save(secretsPath, mat)
		}
	}
	if err != nil {
		return nil, err
	}
	env := &Env{
		Dir: dir, Material: mat,
		Artifacts:     filepath.Join(dir, "artifacts"),
		BootstrapFile: filepath.Join(dir, "bootstrap.token"),
		InternalFile:  filepath.Join(dir, "internal.token"),
		CertFile:      filepath.Join(dir, "cert.pem"),
		KeyFile:       filepath.Join(dir, "key.pem"),
	}
	if err := os.MkdirAll(env.Artifacts, 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(env.InternalFile, []byte(mat.InternalToken+"\n"), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(env.CertFile, []byte(mat.CertPEM), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(env.KeyFile, []byte(mat.KeyPEM), 0o600); err != nil {
		return nil, err
	}
	db, err := store.Open(filepath.Join(dir, "cyandesk.db"))
	if err != nil {
		return nil, err
	}
	env.DB = db
	flag, ferr := db.Meta("bootstrapped")
	if ferr != nil || flag != "1" {
		if _, err := os.Stat(env.BootstrapFile); err != nil {
			raw, err := secret.RandomToken(24)
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(env.BootstrapFile, []byte(raw+"\n"), 0o600); err != nil {
				return nil, err
			}
		}
		raw, err := os.ReadFile(env.BootstrapFile)
		if err != nil {
			return nil, err
		}
		token := string(trimNL(raw))
		if err := db.SetMeta("bootstrap_hash", secret.HashToken(token)); err != nil {
			return nil, err
		}
	}
	return env, nil
}

func trimNL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
