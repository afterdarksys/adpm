package sysscript

import (
	"fmt"
	"os/exec"
	"strings"

	"go.starlark.net/starlark"
)

// =============================================================================
// sys.packages — real implementation (auto-detects apt/dnf/yum/brew/apk)
//
//   sys.packages.install(name)   → {success, output, error}
//   sys.packages.remove(name)    → {success, output, error}
//   sys.packages.update()        → {success, output, error}
//   sys.packages.upgrade(name="")→ {success, output, error}
//   sys.packages.list()          → list of {name, version}
//   sys.packages.search(name)    → list of {name, version, description}
//   sys.packages.backend()       → string (apt|dnf|yum|brew|apk|unknown)
//
// All mutating ops require AllowExec.
// =============================================================================

type pkgBackend struct {
	name    string
	install []string
	remove  []string
	update  []string
	upgrade []string // nil = not supported
	list    []string
	search  []string
	env     []string // extra env vars e.g. DEBIAN_FRONTEND=noninteractive
}

func detectPkgBackend() *pkgBackend {
	type candidate struct {
		binary string
		b      pkgBackend
	}
	candidates := []candidate{
		{"apt-get", pkgBackend{
			name:    "apt",
			install: []string{"apt-get", "install", "-y"},
			remove:  []string{"apt-get", "remove", "-y"},
			update:  []string{"apt-get", "update", "-y"},
			upgrade: []string{"apt-get", "upgrade", "-y"},
			list:    []string{"dpkg-query", "-W", "-f=${Package} ${Version}\n"},
			search:  []string{"apt-cache", "search"},
			env:     []string{"DEBIAN_FRONTEND=noninteractive"},
		}},
		{"dnf", pkgBackend{
			name:    "dnf",
			install: []string{"dnf", "install", "-y"},
			remove:  []string{"dnf", "remove", "-y"},
			update:  []string{"dnf", "makecache"},
			upgrade: []string{"dnf", "upgrade", "-y"},
			list:    []string{"dnf", "list", "installed"},
			search:  []string{"dnf", "search"},
		}},
		{"yum", pkgBackend{
			name:    "yum",
			install: []string{"yum", "install", "-y"},
			remove:  []string{"yum", "remove", "-y"},
			update:  []string{"yum", "makecache"},
			upgrade: []string{"yum", "update", "-y"},
			list:    []string{"yum", "list", "installed"},
			search:  []string{"yum", "search"},
		}},
		{"brew", pkgBackend{
			name:    "brew",
			install: []string{"brew", "install"},
			remove:  []string{"brew", "uninstall"},
			update:  []string{"brew", "update"},
			upgrade: []string{"brew", "upgrade"},
			list:    []string{"brew", "list", "--versions"},
			search:  []string{"brew", "search"},
		}},
		{"apk", pkgBackend{
			name:    "apk",
			install: []string{"apk", "add"},
			remove:  []string{"apk", "del"},
			update:  []string{"apk", "update"},
			upgrade: []string{"apk", "upgrade"},
			list:    []string{"apk", "list", "--installed"},
			search:  []string{"apk", "search"},
		}},
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c.binary); err == nil {
			b := c.b
			return &b
		}
	}
	return &pkgBackend{name: "unknown"}
}

func (engine *Engine) packagesBackend(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return starlark.String(detectPkgBackend().name), nil
}

func (engine *Engine) packagesInstallReal(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for packages.install")
	}
	var name string
	if err := starlark.UnpackArgs("install", args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	pkg := detectPkgBackend()
	if pkg.name == "unknown" {
		return execErrResult("no supported package manager found")
	}
	return execResultWithEnv(append(pkg.install, name), pkg.env)
}

func (engine *Engine) packagesRemove(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for packages.remove")
	}
	var name string
	if err := starlark.UnpackArgs("remove", args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	pkg := detectPkgBackend()
	if pkg.name == "unknown" {
		return execErrResult("no supported package manager found")
	}
	return execResultWithEnv(append(pkg.remove, name), pkg.env)
}

func (engine *Engine) packagesUpdate(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for packages.update")
	}
	pkg := detectPkgBackend()
	if pkg.name == "unknown" {
		return execErrResult("no supported package manager found")
	}
	return execResultWithEnv(pkg.update, pkg.env)
}

func (engine *Engine) packagesUpgrade(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for packages.upgrade")
	}
	var name string
	if err := starlark.UnpackArgs("upgrade", args, kwargs, "name?", &name); err != nil {
		return starlark.None, err
	}
	pkg := detectPkgBackend()
	if pkg.name == "unknown" || pkg.upgrade == nil {
		return execErrResult("upgrade not supported on this backend")
	}
	cmd := pkg.upgrade
	if name != "" {
		cmd = append(cmd, name)
	}
	return execResultWithEnv(cmd, pkg.env)
}

func (engine *Engine) packagesList(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	pkg := detectPkgBackend()
	if pkg.name == "unknown" {
		return starlark.NewList(nil), nil
	}
	cmd := exec.Command(pkg.list[0], pkg.list[1:]...)
	out, err := cmd.Output()
	if err != nil {
		return starlark.NewList(nil), nil
	}
	var results []starlark.Value
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Installed") || strings.HasPrefix(line, "Name") {
			continue
		}
		fields := strings.Fields(line)
		d := starlark.NewDict(2)
		if len(fields) >= 2 {
			d.SetKey(starlark.String("name"), starlark.String(fields[0]))
			d.SetKey(starlark.String("version"), starlark.String(fields[1]))
		} else {
			d.SetKey(starlark.String("name"), starlark.String(fields[0]))
			d.SetKey(starlark.String("version"), starlark.String(""))
		}
		results = append(results, d)
	}
	return starlark.NewList(results), nil
}

func (engine *Engine) packagesSearch(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs("search", args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	pkg := detectPkgBackend()
	if pkg.name == "unknown" {
		return starlark.NewList(nil), nil
	}
	out, err := exec.Command(pkg.search[0], append(pkg.search[1:], name)...).Output()
	if err != nil {
		return starlark.NewList(nil), nil
	}
	var results []starlark.Value
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		d := starlark.NewDict(3)
		d.SetKey(starlark.String("name"), starlark.String(fields[0]))
		if len(fields) > 1 {
			d.SetKey(starlark.String("version"), starlark.String(fields[1]))
		} else {
			d.SetKey(starlark.String("version"), starlark.String(""))
		}
		if len(fields) > 2 {
			d.SetKey(starlark.String("description"), starlark.String(fields[2]))
		} else {
			d.SetKey(starlark.String("description"), starlark.String(""))
		}
		results = append(results, d)
	}
	return starlark.NewList(results), nil
}

// =============================================================================
// sys.containers — Docker CLI wrapper (no SDK dependency)
//
//   sys.containers.ps()                              → list of {id, image, name, status, ports}
//   sys.containers.start(name)                       → {success, output, error}
//   sys.containers.stop(name)                        → {success, output, error}
//   sys.containers.restart(name)                     → {success, output, error}
//   sys.containers.logs(name, lines=100)             → string
//   sys.containers.exec(name, command)               → {stdout, stderr, exit_code, error}
//   sys.containers.pull(image)                       → {success, output, error}
//   sys.containers.inspect(name)                     → dict (parsed JSON)
//   sys.containers.run(image, name="", detach=True, env={}, ports={}, volumes={}) → {success, output, error}
//
// All ops require AllowExec.
// =============================================================================

func (engine *Engine) containersPS(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for containers.ps")
	}
	// docker ps --format "{{json .}}" outputs one JSON object per line
	out, err := exec.Command("docker", "ps", "--format",
		`{"id":"{{.ID}}","image":"{{.Image}}","name":"{{.Names}}","status":"{{.Status}}","ports":"{{.Ports}}"}`).Output()
	if err != nil {
		return starlark.NewList(nil), nil
	}
	var results []starlark.Value
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		val, err := engine.jsonParse(nil, nil,
			starlark.Tuple{starlark.String(line)}, nil)
		if err == nil {
			results = append(results, val)
		}
	}
	return starlark.NewList(results), nil
}

func (engine *Engine) containersStart(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required")
	}
	var name string
	if err := starlark.UnpackArgs("start", args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	return execResult(exec.Command("docker", "start", name))
}

func (engine *Engine) containersStop(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required")
	}
	var name string
	if err := starlark.UnpackArgs("stop", args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	return execResult(exec.Command("docker", "stop", name))
}

func (engine *Engine) containersRestart(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required")
	}
	var name string
	if err := starlark.UnpackArgs("restart", args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	return execResult(exec.Command("docker", "restart", name))
}

func (engine *Engine) containersLogs(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required")
	}
	var name string
	var lines int
	if err := starlark.UnpackArgs("logs", args, kwargs, "name", &name, "lines?", &lines); err != nil {
		return starlark.None, err
	}
	if lines <= 0 {
		lines = 100
	}
	out, _ := exec.Command("docker", "logs", "--tail", fmt.Sprintf("%d", lines), name).CombinedOutput()
	return starlark.String(string(out)), nil
}

func (engine *Engine) containersExec(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required")
	}
	var name, command string
	if err := starlark.UnpackArgs("exec", args, kwargs, "name", &name, "command", &command); err != nil {
		return starlark.None, err
	}
	cmd := exec.Command("docker", "exec", name, "sh", "-c", command)
	return execResult(cmd)
}

func (engine *Engine) containersPull(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required")
	}
	var image string
	if err := starlark.UnpackArgs("pull", args, kwargs, "image", &image); err != nil {
		return starlark.None, err
	}
	return execResult(exec.Command("docker", "pull", image))
}

func (engine *Engine) containersInspect(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required")
	}
	var name string
	if err := starlark.UnpackArgs("inspect", args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	out, err := exec.Command("docker", "inspect", name).Output()
	if err != nil {
		return starlark.None, fmt.Errorf("containers.inspect: %w", err)
	}
	return engine.jsonParse(nil, nil, starlark.Tuple{starlark.String(string(out))}, nil)
}

func (engine *Engine) containersRunReal(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required")
	}
	var image, name string
	var detach bool
	var envVal, portsVal, volumesVal starlark.Value
	if err := starlark.UnpackArgs("run", args, kwargs,
		"image", &image,
		"name?", &name,
		"detach?", &detach,
		"env?", &envVal,
		"ports?", &portsVal,
		"volumes?", &volumesVal,
	); err != nil {
		return starlark.None, err
	}
	if detach {
		// default true
	}

	cmdArgs := []string{"run"}
	if detach {
		cmdArgs = append(cmdArgs, "-d")
	}
	if name != "" {
		cmdArgs = append(cmdArgs, "--name", name)
	}
	if envVal != nil {
		if d, ok := envVal.(*starlark.Dict); ok {
			for _, kv := range d.Items() {
				k, _ := kv[0].(starlark.String)
				v, _ := kv[1].(starlark.String)
				cmdArgs = append(cmdArgs, "-e", fmt.Sprintf("%s=%s", string(k), string(v)))
			}
		}
	}
	if portsVal != nil {
		if d, ok := portsVal.(*starlark.Dict); ok {
			for _, kv := range d.Items() {
				host, _ := kv[0].(starlark.String)
				container, _ := kv[1].(starlark.String)
				cmdArgs = append(cmdArgs, "-p", fmt.Sprintf("%s:%s", string(host), string(container)))
			}
		}
	}
	if volumesVal != nil {
		if d, ok := volumesVal.(*starlark.Dict); ok {
			for _, kv := range d.Items() {
				host, _ := kv[0].(starlark.String)
				container, _ := kv[1].(starlark.String)
				cmdArgs = append(cmdArgs, "-v", fmt.Sprintf("%s:%s", string(host), string(container)))
			}
		}
	}
	cmdArgs = append(cmdArgs, image)
	return execResult(exec.Command("docker", cmdArgs...))
}

// --- helpers ---

func execErrResult(msg string) (starlark.Value, error) {
	d := starlark.NewDict(3)
	d.SetKey(starlark.String("success"), starlark.False)
	d.SetKey(starlark.String("output"), starlark.String(""))
	d.SetKey(starlark.String("error"), starlark.String(msg))
	return d, nil
}

// noErrResult returns execErrResult without the error return for single-value contexts
func noErrResult(msg string) starlark.Value {
	v, _ := execErrResult(msg)
	return v
}

func execResultWithEnv(cmdArgs, env []string) (starlark.Value, error) {
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	return execResult(cmd)
}
