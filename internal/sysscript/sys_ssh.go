package sysscript

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"go.starlark.net/starlark"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// =============================================================================
// sys.ssh — remote execution and file transfer
//
//   sys.ssh.run(host, command, user="root", key="", password="", port=22, timeout_ms=30000)
//     → {stdout, stderr, exit_code, error}
//
//   sys.ssh.copy_to(host, local, remote, user="root", key="", port=22)
//     → {success, error}
//
//   sys.ssh.copy_from(host, remote, local, user="root", key="", port=22)
//     → {success, error}
//
//   sys.ssh.write(host, remote_path, content, user="root", key="", port=22)
//     → {success, error}
//
// Auth priority: explicit key file → SSH_AUTH_SOCK agent → ~/.ssh/id_ed25519 → ~/.ssh/id_rsa
// Host key checking: verified against Entitlements.SSHKnownHostsFile (default
// ~/.ssh/known_hosts). A host with no matching entry is refused unless it is
// explicitly listed in Entitlements.AllowInsecureSSHHostKey.
// =============================================================================

func (engine *Engine) sshAllowed(host string) bool {
	for _, h := range engine.Entitlements.AllowSSH {
		if h == "*" || h == host {
			return true
		}
	}
	return false
}

// sshHostKeyCallback returns the ssh.HostKeyCallback to use for host. Unless
// host is explicitly allow-listed for insecure connections, the returned
// callback verifies against the configured (or default) known_hosts file and
// rejects unknown or mismatched host keys.
func (engine *Engine) sshHostKeyCallback(host string) (ssh.HostKeyCallback, error) {
	for _, h := range engine.Entitlements.AllowInsecureSSHHostKey {
		if h == "*" || h == host {
			return ssh.InsecureIgnoreHostKey(), nil //nolint:gosec — explicit per-host opt-out
		}
	}

	knownHostsPath := engine.Entitlements.SSHKnownHostsFile
	if knownHostsPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("ssh: cannot resolve home directory for known_hosts: %w", err)
		}
		knownHostsPath = filepath.Join(home, ".ssh", "known_hosts")
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf(
			"ssh: load known_hosts %s: %w (add %q to known_hosts, set entitlements.ssh_known_hosts_file, or explicitly allow-list %q under allow_insecure_ssh_host_key)",
			knownHostsPath, err, host, host,
		)
	}
	return callback, nil
}

// sshConnect builds and dials an SSH client.
func (engine *Engine) sshConnect(host, user, keyPath, password string, port, timeoutMs int) (*ssh.Client, error) {
	if user == "" {
		user = "root"
	}
	if port == 0 {
		port = 22
	}
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}

	var authMethods []ssh.AuthMethod

	// 1. Explicit key file
	if keyPath != "" {
		expanded := expandHome(keyPath)
		keyData, err := os.ReadFile(expanded)
		if err != nil {
			return nil, fmt.Errorf("ssh: read key %s: %w", keyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("ssh: parse key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	// 2. SSH agent
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			authMethods = append(authMethods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}

	// 3. Default key files (if no explicit key given)
	if keyPath == "" {
		for _, candidate := range []string{"~/.ssh/id_ed25519", "~/.ssh/id_rsa", "~/.ssh/id_ecdsa"} {
			expanded := expandHome(candidate)
			keyData, err := os.ReadFile(expanded)
			if err != nil {
				continue
			}
			signer, err := ssh.ParsePrivateKey(keyData)
			if err != nil {
				continue
			}
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	// 4. Password
	if password != "" {
		authMethods = append(authMethods, ssh.Password(password))
	}

	hostKeyCallback, err := engine.sshHostKeyCallback(host)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         time.Duration(timeoutMs) * time.Millisecond,
	}

	return ssh.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)), cfg)
}

// --- sys.ssh.run ---

func (engine *Engine) sshRun(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var host, command, user, key, password string
	var port, timeoutMs int
	if err := starlark.UnpackArgs("run", args, kwargs,
		"host", &host, "command", &command,
		"user?", &user, "key?", &key, "password?", &password,
		"port?", &port, "timeout_ms?", &timeoutMs,
	); err != nil {
		return starlark.None, err
	}

	if !engine.sshAllowed(host) {
		return sshErrResult(fmt.Sprintf("security exception: SSH to %s not granted", host)), nil
	}

	client, err := engine.sshConnect(host, user, key, password, port, timeoutMs)
	if err != nil {
		return sshErrResult(err.Error()), nil
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return sshErrResult(err.Error()), nil
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	exitCode := 0
	errMsg := ""
	if runErr := session.Run(command); runErr != nil {
		if exitErr, ok := runErr.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			exitCode = -1
			errMsg = runErr.Error()
		}
	}

	d := starlark.NewDict(4)
	d.SetKey(starlark.String("stdout"), starlark.String(stdoutBuf.String()))
	d.SetKey(starlark.String("stderr"), starlark.String(stderrBuf.String()))
	d.SetKey(starlark.String("exit_code"), starlark.MakeInt(exitCode))
	d.SetKey(starlark.String("error"), starlark.String(errMsg))
	return d, nil
}

// --- sys.ssh.copy_to(host, local, remote, ...) ---

func (engine *Engine) sshCopyTo(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var host, localPath, remotePath, user, key, password string
	var port int
	if err := starlark.UnpackArgs("copy_to", args, kwargs,
		"host", &host, "local", &localPath, "remote", &remotePath,
		"user?", &user, "key?", &key, "password?", &password, "port?", &port,
	); err != nil {
		return starlark.None, err
	}

	if !engine.sshAllowed(host) {
		return sshOkResult(false, fmt.Sprintf("security exception: SSH to %s not granted", host)), nil
	}

	client, err := engine.sshConnect(host, user, key, password, port, 0)
	if err != nil {
		return sshOkResult(false, err.Error()), nil
	}
	defer client.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return sshOkResult(false, fmt.Sprintf("sftp init: %v", err)), nil
	}
	defer sftpClient.Close()

	src, err := os.Open(localPath)
	if err != nil {
		return sshOkResult(false, fmt.Sprintf("open local: %v", err)), nil
	}
	defer src.Close()

	dst, err := sftpClient.Create(remotePath)
	if err != nil {
		return sshOkResult(false, fmt.Sprintf("create remote: %v", err)), nil
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return sshOkResult(false, fmt.Sprintf("copy: %v", err)), nil
	}
	return sshOkResult(true, ""), nil
}

// --- sys.ssh.copy_from(host, remote, local, ...) ---

func (engine *Engine) sshCopyFrom(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var host, remotePath, localPath, user, key, password string
	var port int
	if err := starlark.UnpackArgs("copy_from", args, kwargs,
		"host", &host, "remote", &remotePath, "local", &localPath,
		"user?", &user, "key?", &key, "password?", &password, "port?", &port,
	); err != nil {
		return starlark.None, err
	}

	if !engine.sshAllowed(host) {
		return sshOkResult(false, fmt.Sprintf("security exception: SSH to %s not granted", host)), nil
	}

	client, err := engine.sshConnect(host, user, key, password, port, 0)
	if err != nil {
		return sshOkResult(false, err.Error()), nil
	}
	defer client.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return sshOkResult(false, fmt.Sprintf("sftp init: %v", err)), nil
	}
	defer sftpClient.Close()

	src, err := sftpClient.Open(remotePath)
	if err != nil {
		return sshOkResult(false, fmt.Sprintf("open remote: %v", err)), nil
	}
	defer src.Close()

	dst, err := os.Create(localPath)
	if err != nil {
		return sshOkResult(false, fmt.Sprintf("create local: %v", err)), nil
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return sshOkResult(false, fmt.Sprintf("copy: %v", err)), nil
	}
	return sshOkResult(true, ""), nil
}

// --- sys.ssh.write(host, remote_path, content, ...) ---
// Writes a string directly to a remote file — no local temp file needed.

func (engine *Engine) sshWrite(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var host, remotePath, content, user, key, password string
	var port int
	if err := starlark.UnpackArgs("write", args, kwargs,
		"host", &host, "remote_path", &remotePath, "content", &content,
		"user?", &user, "key?", &key, "password?", &password, "port?", &port,
	); err != nil {
		return starlark.None, err
	}

	if !engine.sshAllowed(host) {
		return sshOkResult(false, fmt.Sprintf("security exception: SSH to %s not granted", host)), nil
	}

	client, err := engine.sshConnect(host, user, key, password, port, 0)
	if err != nil {
		return sshOkResult(false, err.Error()), nil
	}
	defer client.Close()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return sshOkResult(false, fmt.Sprintf("sftp init: %v", err)), nil
	}
	defer sftpClient.Close()

	f, err := sftpClient.Create(remotePath)
	if err != nil {
		return sshOkResult(false, fmt.Sprintf("create remote: %v", err)), nil
	}
	defer f.Close()

	if _, err := f.Write([]byte(content)); err != nil {
		return sshOkResult(false, fmt.Sprintf("write: %v", err)), nil
	}
	return sshOkResult(true, ""), nil
}

// --- helpers ---

func sshErrResult(msg string) starlark.Value {
	d := starlark.NewDict(4)
	d.SetKey(starlark.String("stdout"), starlark.String(""))
	d.SetKey(starlark.String("stderr"), starlark.String(""))
	d.SetKey(starlark.String("exit_code"), starlark.MakeInt(-1))
	d.SetKey(starlark.String("error"), starlark.String(msg))
	return d
}

func sshOkResult(success bool, errMsg string) starlark.Value {
	d := starlark.NewDict(2)
	d.SetKey(starlark.String("success"), starlark.Bool(success))
	d.SetKey(starlark.String("error"), starlark.String(errMsg))
	return d
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + path[1:]
		}
	}
	return path
}
