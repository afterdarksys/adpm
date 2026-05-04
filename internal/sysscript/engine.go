package sysscript

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Entitlements define what the current script is permitted to do.
type Entitlements struct {
	AllowFSWrite       []string `json:"allow_fs_write"`
	AllowFSRead        []string `json:"allow_fs_read"`
	AllowNetOutbound   []string `json:"allow_net_outbound"`
	AllowExec          bool     `json:"allow_exec"`
	AllowConfigWrite   []string `json:"allow_config_write"`   // paths allowed for atomic config writes
	AllowServiceReload []string `json:"allow_service_reload"` // service names allowed to reload
	AllowSSH           []string `json:"allow_ssh"`            // hosts/IPs allowed for sys.ssh (supports *)
}

// Engine wraps Starlark execution with a full sys.* API surface.
type Engine struct {
	Entitlements *Entitlements
	cm           *ConfigManager
}

// New returns an Engine with the given entitlements (nil = deny all).
func New(perms *Entitlements) *Engine {
	if perms == nil {
		perms = &Entitlements{}
	}
	return &Engine{Entitlements: perms, cm: &ConfigManager{}}
}

// Execute runs Starlark source to completion and returns printed output.
func (engine *Engine) Execute(scriptSource string) (string, error) {
	var outBuffer bytes.Buffer

	thread := &starlark.Thread{
		Name:  "sysscript",
		Print: func(_ *starlark.Thread, msg string) { outBuffer.WriteString(msg + "\n") },
	}

	predeclared := starlark.StringDict{
		"sys": engine.buildSysModule(),
	}

	_, err := starlark.ExecFile(thread, "script.star", scriptSource, predeclared)
	if err != nil {
		return outBuffer.String(), err
	}

	return outBuffer.String(), nil
}

// ExecuteFile reads a .star file and runs it.
func (engine *Engine) ExecuteFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read script: %w", err)
	}
	return engine.Execute(string(data))
}

// buildSysModule assembles the sys.* object tree.
func (engine *Engine) buildSysModule() *starlarkstruct.Struct {

	// sys.net
	sysNet := starlark.StringDict{
		"http_get":     starlark.NewBuiltin("http_get", engine.netHTTPGet),
		"http_post":    starlark.NewBuiltin("http_post", engine.netHTTPPost),
		"http_request": starlark.NewBuiltin("http_request", engine.netHTTPRequest),
		"dns_lookup":   starlark.NewBuiltin("dns_lookup", engine.netDNSLookup),
		"reverse_dns":  starlark.NewBuiltin("reverse_dns", engine.netReverseDNS),
		"port_check":   starlark.NewBuiltin("port_check", engine.netPortCheck),
	}

	// sys.exec
	sysExec := starlark.StringDict{
		"run": starlark.NewBuiltin("run", engine.execRun),
	}

	// sys.fs
	sysFS := starlark.StringDict{
		"read":  starlark.NewBuiltin("read", engine.fsRead),
		"write": starlark.NewBuiltin("write", engine.fsWrite),
		"stat":  starlark.NewBuiltin("stat", engine.fsStat),
		"glob":  starlark.NewBuiltin("glob", engine.fsGlob),
		"mkdir": starlark.NewBuiltin("mkdir", engine.fsMkdir),
		"rm":    starlark.NewBuiltin("rm", engine.fsRm),
		"chmod": starlark.NewBuiltin("chmod", engine.fsChmod),
	}

	// sys.config
	sysConfig := starlark.StringDict{
		"write":    starlark.NewBuiltin("write", engine.configWrite),
		"template": starlark.NewBuiltin("template", engine.configTemplate),
		"validate": starlark.NewBuiltin("validate", engine.configValidate),
		"reload":   starlark.NewBuiltin("reload", engine.configReload),
		"backup":   starlark.NewBuiltin("backup", engine.configBackup),
		"restore":  starlark.NewBuiltin("restore", engine.configRestore),
	}

	// sys.yaml
	sysYAML := starlark.StringDict{
		"parse":  starlark.NewBuiltin("parse", engine.yamlParse),
		"encode": starlark.NewBuiltin("encode", engine.yamlEncode),
	}

	// sys.json
	sysJSON := starlark.StringDict{
		"parse":        starlark.NewBuiltin("parse", engine.jsonParse),
		"encode":       starlark.NewBuiltin("encode", engine.jsonEncode),
		"encode_pretty": starlark.NewBuiltin("encode_pretty", engine.jsonEncodePretty),
	}

	// sys.ini
	sysINI := starlark.StringDict{
		"parse":  starlark.NewBuiltin("parse", engine.iniParse),
		"encode": starlark.NewBuiltin("encode", engine.iniEncode),
		"get":    starlark.NewBuiltin("get", engine.iniGet),
	}

	// sys.proc
	sysProc := starlark.StringDict{
		"list":  starlark.NewBuiltin("list", engine.procList),
		"get":   starlark.NewBuiltin("get", engine.procGet),
		"find":  starlark.NewBuiltin("find", engine.procFind),
		"kill":  starlark.NewBuiltin("kill", engine.procKill),
	}

	// sys.services
	sysSvc := starlark.StringDict{
		"start":   starlark.NewBuiltin("start", engine.svcStart),
		"stop":    starlark.NewBuiltin("stop", engine.svcStop),
		"restart": starlark.NewBuiltin("restart", engine.svcRestart),
		"enable":  starlark.NewBuiltin("enable", engine.svcEnable),
		"disable": starlark.NewBuiltin("disable", engine.svcDisable),
		"status":  starlark.NewBuiltin("status", engine.svcStatus),
	}

	// sys.alerts (stub — wire to fleet manager in implementing project)
	sysAlerts := starlark.StringDict{
		"push": starlark.NewBuiltin("push", engine.alertsPush),
	}

	// sys.security (stub — wire YARA/memory forensics)
	sysSecurity := starlark.StringDict{
		"yara_scan":   starlark.NewBuiltin("yara_scan", engine.securityYaraScan),
		"scan_memory": starlark.NewBuiltin("scan_memory", engine.securityScanMemory),
	}

	// sys.events (stub — wire eBPF/ETW)
	sysEvents := starlark.StringDict{
		"listen": starlark.NewBuiltin("listen", engine.eventsListen),
	}

	// sys.packages
	sysPackages := starlark.StringDict{
		"install": starlark.NewBuiltin("install", engine.packagesInstallReal),
		"remove":  starlark.NewBuiltin("remove", engine.packagesRemove),
		"update":  starlark.NewBuiltin("update", engine.packagesUpdate),
		"upgrade": starlark.NewBuiltin("upgrade", engine.packagesUpgrade),
		"list":    starlark.NewBuiltin("list", engine.packagesList),
		"search":  starlark.NewBuiltin("search", engine.packagesSearch),
		"backend": starlark.NewBuiltin("backend", engine.packagesBackend),
	}

	// sys.containers
	sysContainers := starlark.StringDict{
		"ps":      starlark.NewBuiltin("ps", engine.containersPS),
		"start":   starlark.NewBuiltin("start", engine.containersStart),
		"stop":    starlark.NewBuiltin("stop", engine.containersStop),
		"restart": starlark.NewBuiltin("restart", engine.containersRestart),
		"logs":    starlark.NewBuiltin("logs", engine.containersLogs),
		"exec":    starlark.NewBuiltin("exec", engine.containersExec),
		"pull":    starlark.NewBuiltin("pull", engine.containersPull),
		"inspect": starlark.NewBuiltin("inspect", engine.containersInspect),
		"run":     starlark.NewBuiltin("run", engine.containersRunReal),
	}

	// sys.disk
	sysDisk := starlark.StringDict{
		"usage":      starlark.NewBuiltin("usage", engine.diskUsage),
		"partitions": starlark.NewBuiltin("partitions", engine.diskPartitions),
		"io":         starlark.NewBuiltin("io", engine.diskIO),
	}

	// sys.network
	sysNetwork := starlark.StringDict{
		"interfaces":  starlark.NewBuiltin("interfaces", engine.networkInterfaces),
		"io":          starlark.NewBuiltin("io", engine.networkIO),
		"connections": starlark.NewBuiltin("connections", engine.networkConnections),
		"routes":      starlark.NewBuiltin("routes", engine.networkRoutes),
	}

	// sys.accounts
	sysAccounts := starlark.StringDict{
		"list_users":   starlark.NewBuiltin("list_users", engine.accountsListUsers),
		"list_groups":  starlark.NewBuiltin("list_groups", engine.accountsListGroups),
		"add_user":     starlark.NewBuiltin("add_user", engine.accountsAddUser),
		"del_user":     starlark.NewBuiltin("del_user", engine.accountsDelUser),
		"add_group":    starlark.NewBuiltin("add_group", engine.accountsAddGroup),
		"del_group":    starlark.NewBuiltin("del_group", engine.accountsDelGroup),
		"set_password": starlark.NewBuiltin("set_password", engine.accountsSetPassword),
		"id":           starlark.NewBuiltin("id", engine.accountsID),
	}

	// sys.cron
	sysCron := starlark.StringDict{
		"list":   starlark.NewBuiltin("list", engine.cronList),
		"add":    starlark.NewBuiltin("add", engine.cronAdd),
		"remove": starlark.NewBuiltin("remove", engine.cronRemove),
		"write":  starlark.NewBuiltin("write", engine.cronWrite),
	}

	// sys.firewall
	sysFirewall := starlark.StringDict{
		"status": starlark.NewBuiltin("status", engine.firewallStatus),
		"list":   starlark.NewBuiltin("list", engine.firewallList),
		"allow":  starlark.NewBuiltin("allow", engine.firewallAllow),
		"deny":   starlark.NewBuiltin("deny", engine.firewallDeny),
		"delete": starlark.NewBuiltin("delete", engine.firewallDelete),
	}

	// sys.ssl
	sysSSL := starlark.StringDict{
		"inspect":           starlark.NewBuiltin("inspect", engine.sslInspect),
		"inspect_file":      starlark.NewBuiltin("inspect_file", engine.sslInspectFile),
		"days_until_expiry": starlark.NewBuiltin("days_until_expiry", engine.sslDaysUntilExpiry),
		"verify":            starlark.NewBuiltin("verify", engine.sslVerify),
	}

	// sys.ssh
	sysSSH := starlark.StringDict{
		"run":       starlark.NewBuiltin("run", engine.sshRun),
		"copy_to":   starlark.NewBuiltin("copy_to", engine.sshCopyTo),
		"copy_from": starlark.NewBuiltin("copy_from", engine.sshCopyFrom),
		"write":     starlark.NewBuiltin("write", engine.sshWrite),
	}

	sysDict := starlark.StringDict{
		"net":        starlarkstruct.FromStringDict(starlark.String("net"), sysNet),
		"exec":       starlarkstruct.FromStringDict(starlark.String("exec"), sysExec),
		"fs":         starlarkstruct.FromStringDict(starlark.String("fs"), sysFS),
		"config":     starlarkstruct.FromStringDict(starlark.String("config"), sysConfig),
		"yaml":       starlarkstruct.FromStringDict(starlark.String("yaml"), sysYAML),
		"json":       starlarkstruct.FromStringDict(starlark.String("json"), sysJSON),
		"ini":        starlarkstruct.FromStringDict(starlark.String("ini"), sysINI),
		"proc":       starlarkstruct.FromStringDict(starlark.String("proc"), sysProc),
		"services":   starlarkstruct.FromStringDict(starlark.String("services"), sysSvc),
		"ssh":        starlarkstruct.FromStringDict(starlark.String("ssh"), sysSSH),
		"alerts":     starlarkstruct.FromStringDict(starlark.String("alerts"), sysAlerts),
		"security":   starlarkstruct.FromStringDict(starlark.String("security"), sysSecurity),
		"events":     starlarkstruct.FromStringDict(starlark.String("events"), sysEvents),
		"packages":   starlarkstruct.FromStringDict(starlark.String("packages"), sysPackages),
		"containers": starlarkstruct.FromStringDict(starlark.String("containers"), sysContainers),
		"disk":       starlarkstruct.FromStringDict(starlark.String("disk"), sysDisk),
		"network":    starlarkstruct.FromStringDict(starlark.String("network"), sysNetwork),
		"accounts":   starlarkstruct.FromStringDict(starlark.String("accounts"), sysAccounts),
		"cron":       starlarkstruct.FromStringDict(starlark.String("cron"), sysCron),
		"firewall":   starlarkstruct.FromStringDict(starlark.String("firewall"), sysFirewall),
		"ssl":        starlarkstruct.FromStringDict(starlark.String("ssl"), sysSSL),
	}

	return starlarkstruct.FromStringDict(starlark.String("sys"), sysDict)
}

// --- sys.exec.run(command, args...) ---

func (engine *Engine) execRun(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement not granted")
	}
	if args.Len() < 1 {
		return starlark.None, fmt.Errorf("sys.exec.run requires at least one argument")
	}
	cmdStr, ok := args.Index(0).(starlark.String)
	if !ok {
		return starlark.None, fmt.Errorf("expected string for command")
	}
	var cmdArgs []string
	for i := 1; i < args.Len(); i++ {
		if s, ok := args.Index(i).(starlark.String); ok {
			cmdArgs = append(cmdArgs, string(s))
		}
	}
	cmd := exec.Command(string(cmdStr), cmdArgs...)
	out, err := cmd.CombinedOutput()
	d := starlark.NewDict(2)
	if err != nil {
		d.SetKey(starlark.String("error"), starlark.String(err.Error()))
	} else {
		d.SetKey(starlark.String("error"), starlark.None)
	}
	d.SetKey(starlark.String("output"), starlark.String(string(out)))
	return d, nil
}

// --- sys.net.http_get(url) ---

func (engine *Engine) netHTTPGet(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var reqURL string
	if err := starlark.UnpackArgs("http_get", args, kwargs, "url", &reqURL); err != nil {
		return starlark.None, err
	}
	u, err := url.Parse(reqURL)
	if err != nil {
		return starlark.None, fmt.Errorf("invalid url: %v", err)
	}
	allowed := false
	for _, domain := range engine.Entitlements.AllowNetOutbound {
		if domain == "*" || domain == u.Hostname() {
			allowed = true
			break
		}
	}
	if !allowed {
		return starlark.None, fmt.Errorf("security exception: outbound to %s not granted", u.Hostname())
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(reqURL)
	if err != nil {
		return starlark.None, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	d := starlark.NewDict(2)
	d.SetKey(starlark.String("status_code"), starlark.MakeInt(resp.StatusCode))
	d.SetKey(starlark.String("body"), starlark.String(string(body)))
	return d, nil
}

// --- sys.fs.read(path) ---

func (engine *Engine) fsRead(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("read", args, kwargs, "path", &path); err != nil {
		return starlark.None, err
	}
	allowed := false
	for _, prefix := range engine.Entitlements.AllowFSRead {
		if prefix == "*" || strings.HasPrefix(path, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		return starlark.None, fmt.Errorf("security exception: read to %s not granted", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return starlark.None, err
	}
	return starlark.String(string(data)), nil
}

// --- sys.fs.write(path, content) ---

func (engine *Engine) fsWrite(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, content string
	if err := starlark.UnpackArgs("write", args, kwargs, "path", &path, "content", &content); err != nil {
		return starlark.None, err
	}
	allowed := false
	for _, prefix := range engine.Entitlements.AllowFSWrite {
		if prefix == "*" || strings.HasPrefix(path, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		return starlark.None, fmt.Errorf("security exception: write to %s not granted", path)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return starlark.None, err
	}
	return starlark.True, nil
}
