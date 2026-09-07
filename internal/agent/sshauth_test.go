package agent

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateClientSSHAuthSock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		path    func(t *testing.T) string
		wantErr string
	}{
		{name: "empty", path: func(*testing.T) string { return "" }, wantErr: "SSH_AUTH_SOCK is not set"},
		{name: "missing", path: func(t *testing.T) string {
			return filepath.Join(t.TempDir(), "nope.sock")
		}, wantErr: "SSH_AUTH_SOCK"},
		{name: "regular file", path: func(t *testing.T) string {
			p := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}, wantErr: "not a unix socket"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateClientSSHAuthSock(tt.path(t))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err=%v want substring %q", err, tt.wantErr)
			}
		})
	}

	t.Run("unix socket", func(t *testing.T) {
		t.Parallel()
		dir, err := os.MkdirTemp("", "gf-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		p := filepath.Join(dir, "s")
		ln, err := net.Listen("unix", p)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		if err := ValidateClientSSHAuthSock(p); err != nil {
			t.Fatal(err)
		}
	})
}
