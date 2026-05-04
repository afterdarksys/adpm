package sysscript

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.starlark.net/starlark"
)

// =============================================================================
// sys.firewall — iptables/ufw rule management
//
//   sys.firewall.status()                             → {backend, active, output}
//   sys.firewall.list()                               → raw rules string
//   sys.firewall.allow(port, proto="tcp", from="any") → {success, error}
//   sys.firewall.deny(port, proto="tcp", from="any")  → {success, error}
//   sys.firewall.delete(rule)                         → {success, error}
//
// All mutating ops require AllowExec.
// Backend auto-detected: ufw (if ufw is installed) → iptables.
// =============================================================================

func detectFirewallBackend() string {
	if _, err := exec.LookPath("ufw"); err == nil {
		return "ufw"
	}
	if _, err := exec.LookPath("iptables"); err == nil {
		return "iptables"
	}
	if _, err := exec.LookPath("nft"); err == nil {
		return "nftables"
	}
	return "unknown"
}

func (engine *Engine) firewallStatus(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	backend := detectFirewallBackend()
	d := starlark.NewDict(3)
	d.SetKey(starlark.String("backend"), starlark.String(backend))

	var out []byte
	var active bool
	switch backend {
	case "ufw":
		raw, _ := exec.Command("ufw", "status", "verbose").Output()
		out = raw
		active = strings.Contains(string(raw), "Status: active")
	case "iptables":
		raw, _ := exec.Command("iptables", "-L", "-n", "--line-numbers").Output()
		out = raw
		active = true
	case "nftables":
		raw, _ := exec.Command("nft", "list", "ruleset").Output()
		out = raw
		active = len(raw) > 0
	default:
		d.SetKey(starlark.String("active"), starlark.False)
		d.SetKey(starlark.String("output"), starlark.String("no supported firewall detected"))
		return d, nil
	}

	d.SetKey(starlark.String("active"), starlark.Bool(active))
	d.SetKey(starlark.String("output"), starlark.String(strings.TrimSpace(string(out))))
	return d, nil
}

func (engine *Engine) firewallList(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	backend := detectFirewallBackend()
	var out []byte
	switch backend {
	case "ufw":
		out, _ = exec.Command("ufw", "status", "numbered").Output()
	case "iptables":
		out, _ = exec.Command("iptables-save").Output()
	case "nftables":
		out, _ = exec.Command("nft", "list", "ruleset").Output()
	default:
		return starlark.String("no supported firewall detected"), nil
	}
	return starlark.String(strings.TrimSpace(string(out))), nil
}

func (engine *Engine) firewallAllow(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for firewall.allow")
	}
	var port, proto, from string
	if err := starlark.UnpackArgs("allow", args, kwargs, "port", &port, "proto?", &proto, "from?", &from); err != nil {
		return starlark.None, err
	}
	if proto == "" {
		proto = "tcp"
	}
	return firewallMutate("allow", port, proto, from)
}

func (engine *Engine) firewallDeny(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for firewall.deny")
	}
	var port, proto, from string
	if err := starlark.UnpackArgs("deny", args, kwargs, "port", &port, "proto?", &proto, "from?", &from); err != nil {
		return starlark.None, err
	}
	if proto == "" {
		proto = "tcp"
	}
	return firewallMutate("deny", port, proto, from)
}

func (engine *Engine) firewallDelete(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for firewall.delete")
	}
	var rule string
	if err := starlark.UnpackArgs("delete", args, kwargs, "rule", &rule); err != nil {
		return starlark.None, err
	}
	backend := detectFirewallBackend()
	switch backend {
	case "ufw":
		return execResult(exec.Command("ufw", "delete", rule))
	case "iptables":
		// rule is expected as a chain + rule number e.g. "INPUT 3"
		parts := strings.Fields(rule)
		if len(parts) != 2 {
			return execResult(exec.Command("iptables", "-D", rule))
		}
		return execResult(exec.Command("iptables", "-D", parts[0], parts[1]))
	default:
		d := starlark.NewDict(2)
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("error"), starlark.String("unsupported firewall backend"))
		return d, nil
	}
}

func firewallMutate(action, port, proto, from string) (starlark.Value, error) {
	backend := detectFirewallBackend()
	switch backend {
	case "ufw":
		if from != "" && from != "any" {
			return execResult(exec.Command("ufw", action, "from", from, "to", "any", "port", port, "proto", proto))
		}
		return execResult(exec.Command("ufw", action, port+"/"+proto))
	case "iptables":
		iptAction := "-A"
		iptTarget := "ACCEPT"
		if action == "deny" {
			iptTarget = "DROP"
		}
		cmdArgs := []string{iptAction, "INPUT", "-p", proto, "--dport", port}
		if from != "" && from != "any" {
			cmdArgs = append(cmdArgs, "-s", from)
		}
		cmdArgs = append(cmdArgs, "-j", iptTarget)
		return execResult(exec.Command("iptables", cmdArgs...))
	default:
		d := starlark.NewDict(2)
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("error"), starlark.String("unsupported firewall backend: "+backend))
		return d, nil
	}
}

// =============================================================================
// sys.ssl — TLS certificate inspection
//
//   sys.ssl.inspect(host, port=443, timeout_ms=10000) → cert dict
//   sys.ssl.inspect_file(path)                        → cert dict
//   sys.ssl.days_until_expiry(host, port=443)         → int
//   sys.ssl.verify(host, port=443)                    → {valid, error}
//
// cert dict: {subject, issuer, dns_names, not_before, not_after, serial, expired, days_remaining}
// =============================================================================

func (engine *Engine) sslInspect(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var host string
	var port, timeoutMs int
	if err := starlark.UnpackArgs("inspect", args, kwargs, "host", &host, "port?", &port, "timeout_ms?", &timeoutMs); err != nil {
		return starlark.None, err
	}
	if port == 0 {
		port = 443
	}
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}
	cfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec — intentional for inspection
	conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%d", host, port), cfg)
	if err != nil {
		return starlark.None, fmt.Errorf("ssl.inspect: %w", err)
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return starlark.None, fmt.Errorf("ssl.inspect: no certificates returned")
	}
	return certToDict(certs[0]), nil
}

func (engine *Engine) sslInspectFile(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("inspect_file", args, kwargs, "path", &path); err != nil {
		return starlark.None, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return starlark.None, fmt.Errorf("ssl.inspect_file: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return starlark.None, fmt.Errorf("ssl.inspect_file: no PEM block found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return starlark.None, fmt.Errorf("ssl.inspect_file: %w", err)
	}
	return certToDict(cert), nil
}

func (engine *Engine) sslDaysUntilExpiry(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var host string
	var port int
	if err := starlark.UnpackArgs("days_until_expiry", args, kwargs, "host", &host, "port?", &port); err != nil {
		return starlark.None, err
	}
	if port == 0 {
		port = 443
	}
	cfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%d", host, port), cfg)
	if err != nil {
		return starlark.None, fmt.Errorf("ssl.days_until_expiry: %w", err)
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return starlark.None, fmt.Errorf("ssl.days_until_expiry: no certificates returned")
	}
	days := int(time.Until(certs[0].NotAfter).Hours() / 24)
	return starlark.MakeInt(days), nil
}

func (engine *Engine) sslVerify(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var host string
	var port int
	if err := starlark.UnpackArgs("verify", args, kwargs, "host", &host, "port?", &port); err != nil {
		return starlark.None, err
	}
	if port == 0 {
		port = 443
	}
	// Use strict verification (no InsecureSkipVerify)
	conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%d", host, port), &tls.Config{ServerName: host})
	d := starlark.NewDict(2)
	if err != nil {
		d.SetKey(starlark.String("valid"), starlark.False)
		d.SetKey(starlark.String("error"), starlark.String(err.Error()))
	} else {
		conn.Close()
		d.SetKey(starlark.String("valid"), starlark.True)
		d.SetKey(starlark.String("error"), starlark.String(""))
	}
	return d, nil
}

func certToDict(cert *x509.Certificate) starlark.Value {
	dnsNames := make([]starlark.Value, len(cert.DNSNames))
	for i, n := range cert.DNSNames {
		dnsNames[i] = starlark.String(n)
	}
	now := time.Now()
	daysRemaining := int(cert.NotAfter.Sub(now).Hours() / 24)

	d := starlark.NewDict(8)
	d.SetKey(starlark.String("subject"), starlark.String(cert.Subject.CommonName))
	d.SetKey(starlark.String("issuer"), starlark.String(cert.Issuer.CommonName))
	d.SetKey(starlark.String("dns_names"), starlark.NewList(dnsNames))
	d.SetKey(starlark.String("not_before"), starlark.String(cert.NotBefore.UTC().Format(time.RFC3339)))
	d.SetKey(starlark.String("not_after"), starlark.String(cert.NotAfter.UTC().Format(time.RFC3339)))
	d.SetKey(starlark.String("serial"), starlark.String(cert.SerialNumber.String()))
	d.SetKey(starlark.String("expired"), starlark.Bool(now.After(cert.NotAfter)))
	d.SetKey(starlark.String("days_remaining"), starlark.MakeInt(daysRemaining))
	return d
}
