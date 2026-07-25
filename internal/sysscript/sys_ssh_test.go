package sysscript

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// fakeAddr is a minimal net.Addr for exercising ssh.HostKeyCallback in tests;
// the knownhosts checker dereferences the address, so nil is not valid here.
type fakeAddr struct{}

func (fakeAddr) Network() string { return "tcp" }
func (fakeAddr) String() string  { return "example.com:22" }

var _ net.Addr = fakeAddr{}

// These tests guard against a regression back to ssh.InsecureIgnoreHostKey()
// being the unconditional default (the bug this file's sibling fixed).

func TestSSHHostKeyCallback_FailsClosedWithoutKnownHosts(t *testing.T) {
	engine := New(&Entitlements{
		SSHKnownHostsFile: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if _, err := engine.sshHostKeyCallback("example.com"); err == nil {
		t.Fatal("expected an error when known_hosts is missing and no insecure opt-out is configured")
	}
}

func TestSSHHostKeyCallback_RejectsUnknownHostEvenWithValidKnownHostsFile(t *testing.T) {
	// A valid, but empty, known_hosts file. No host in AllowInsecureSSHHostKey.
	dir := t.TempDir()
	khPath := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(khPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := New(&Entitlements{SSHKnownHostsFile: khPath})

	callback, err := engine.sshHostKeyCallback("example.com")
	if err != nil {
		t.Fatalf("expected callback construction to succeed, got: %v", err)
	}

	// Generate a throwaway key to present as the "server" host key.
	signer := testSigner(t)
	verifyErr := callback("example.com:22", fakeAddr{}, signer.PublicKey())
	if verifyErr == nil {
		t.Fatal("expected verification to fail for a host with no known_hosts entry")
	}
}

func TestSSHHostKeyCallback_InsecureOptOutIsPerHostOnly(t *testing.T) {
	engine := New(&Entitlements{
		SSHKnownHostsFile:       filepath.Join(t.TempDir(), "does-not-exist"),
		AllowInsecureSSHHostKey: []string{"allowed.example.com"},
	})

	if _, err := engine.sshHostKeyCallback("allowed.example.com"); err != nil {
		t.Fatalf("allow-listed host should get an insecure callback without error, got: %v", err)
	}

	if _, err := engine.sshHostKeyCallback("other.example.com"); err == nil {
		t.Fatal("a host NOT on the allow-list must still fail closed")
	}
}

func TestSSHHostKeyCallback_WildcardOptOutAppliesToAnyHost(t *testing.T) {
	engine := New(&Entitlements{
		AllowInsecureSSHHostKey: []string{"*"},
	})
	if _, err := engine.sshHostKeyCallback("literally.any.host"); err != nil {
		t.Fatalf("wildcard opt-out should apply to any host, got: %v", err)
	}
}

func testSigner(t *testing.T) ssh.Signer {
	t.Helper()
	// The test only needs a valid public key to present to the host-key
	// callback, not a real identity, so a freshly generated key is fine.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("build test signer: %v", err)
	}
	return signer
}
