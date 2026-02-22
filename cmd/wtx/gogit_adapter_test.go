package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

func TestExpandSSHIdentityPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "localdev")

	got := expandSSHIdentityPath("%h-%r-%u-key", "github.com", "git")
	want := filepath.Join(home, ".ssh", "github.com-git-localdev-key")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestSSHIdentityFiles_ConfigThenDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "localdev")

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	custom := filepath.Join(sshDir, "custom")
	idEd := filepath.Join(sshDir, "id_ed25519")
	idRSA := filepath.Join(sshDir, "id_rsa")
	for _, p := range []string{custom, idEd, idRSA} {
		if err := os.WriteFile(p, []byte("key"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	oldGetAll := sshConfigGetAll
	sshConfigGetAll = func(alias, key string) []string {
		return []string{"~/.ssh/custom", "none", "/does/not/exist"}
	}
	t.Cleanup(func() { sshConfigGetAll = oldGetAll })

	got := sshIdentityFiles("github.com", "git")
	if len(got) < 3 {
		t.Fatalf("expected at least 3 key paths, got %v", got)
	}
	if got[0] != custom {
		t.Fatalf("expected first key to be config custom key %q, got %q", custom, got[0])
	}
	if got[1] != idEd {
		t.Fatalf("expected second key to be default id_ed25519 %q, got %q", idEd, got[1])
	}
	if got[2] != idRSA {
		t.Fatalf("expected third key to be default id_rsa %q, got %q", idRSA, got[2])
	}
}

func TestSSHAuthMethodForEndpoint_KeyFileFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USER", "localdev")
	t.Setenv("SSH_AUTH_SOCK", "")

	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	keyPath := filepath.Join(sshDir, "custom")
	if err := writeTestPrivateKey(t, keyPath); err != nil {
		t.Fatalf("write private key: %v", err)
	}

	oldGet := sshConfigGet
	oldGetAll := sshConfigGetAll
	sshConfigGet = func(alias, key string) string {
		if key == "User" {
			return "git"
		}
		return ""
	}
	sshConfigGetAll = func(alias, key string) []string {
		return []string{"~/.ssh/custom"}
	}
	t.Cleanup(func() {
		sshConfigGet = oldGet
		sshConfigGetAll = oldGetAll
	})

	endpoint := &transport.Endpoint{
		Protocol: "ssh",
		Host:     "github.com",
	}
	auth, _, err := sshAuthMethodForEndpoint(endpoint, "git@github.com:mrbonezy/wtx.git")
	if err != nil {
		t.Fatalf("expected fallback auth, got error: %v", err)
	}
	if auth == nil {
		t.Fatalf("expected non-nil auth method")
	}
}

func TestIsSSHAuthFailure(t *testing.T) {
	cases := []struct {
		errText string
		want    bool
	}{
		{errText: "ssh: unable to authenticate, attempted methods [none publickey], no supported methods remain", want: true},
		{errText: "Permission denied (publickey).", want: true},
		{errText: "repository not found", want: false},
	}
	for _, tc := range cases {
		got := isSSHAuthFailure(assertErr(tc.errText))
		if got != tc.want {
			t.Fatalf("isSSHAuthFailure(%q)=%v, want %v", tc.errText, got, tc.want)
		}
	}
}

func writeTestPrivateKey(t *testing.T, path string) error {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}
	data := pem.EncodeToMemory(block)
	return os.WriteFile(path, data, 0o600)
}

func assertErr(text string) error {
	return &testErr{text: text}
}

type testErr struct {
	text string
}

func (e *testErr) Error() string {
	return e.text
}
