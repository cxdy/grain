package sshkey

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// Ensure generates an ed25519 keypair under dataDir/ssh if missing.
// Returns path to private key and authorized_keys line (public).
func Ensure(dataDir string) (privPath, pubLine string, err error) {
	dir := filepath.Join(dataDir, "ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	privPath = filepath.Join(dir, "id_grain")
	pubPath := filepath.Join(dir, "id_grain.pub")

	if _, err := os.Stat(privPath); err == nil {
		b, err := os.ReadFile(pubPath)
		if err != nil {
			return "", "", err
		}
		return privPath, bytesTrim(b), nil
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	// OpenSSH private key format
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		// fallback: put raw PKCS8-ish via ssh
		return "", "", fmt.Errorf("marshal private key: %w", err)
	}
	if err := os.WriteFile(privPath, pem.EncodeToMemory(block), 0o600); err != nil {
		return "", "", err
	}
	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		return "", "", err
	}
	line := string(ssh.MarshalAuthorizedKey(pub))
	if err := os.WriteFile(pubPath, []byte(line), 0o644); err != nil {
		return "", "", err
	}
	return privPath, bytesTrim([]byte(line)), nil
}

func bytesTrim(b []byte) string {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return string(b)
}
