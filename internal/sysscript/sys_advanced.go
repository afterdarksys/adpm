package sysscript

import (
	"fmt"
	"log"

	"go.starlark.net/starlark"
)

// =============================================================================
// sys.alerts  — wire to fleet manager / notification backend in production
// =============================================================================

func (engine *Engine) alertsPush(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var severity, message string
	if err := starlark.UnpackArgs("push", args, kwargs, "severity", &severity, "message", &message); err != nil {
		return starlark.None, err
	}
	log.Printf("[sysscript alert] severity=%s message=%s", severity, message)
	// TODO: wire to WebSocket / alerting backend
	return starlark.True, nil
}

// =============================================================================
// sys.security  — YARA / memory forensics stubs
// =============================================================================

func (engine *Engine) securityYaraScan(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("yara_scan", args, kwargs, "path", &path); err != nil {
		return starlark.None, err
	}
	// TODO: wire YARA cgo / external scanner
	return starlark.String(fmt.Sprintf("stub: yara_scan %s", path)), nil
}

func (engine *Engine) securityScanMemory(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var pid int
	if err := starlark.UnpackArgs("scan_memory", args, kwargs, "pid", &pid); err != nil {
		return starlark.None, err
	}
	// TODO: wire memory forensics
	return starlark.String(fmt.Sprintf("stub: scan_memory pid=%d", pid)), nil
}

// =============================================================================
// sys.events  — eBPF / ETW stub
// =============================================================================

func (engine *Engine) eventsListen(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var eventType string
	var callback starlark.Callable
	if err := starlark.UnpackArgs("listen", args, kwargs, "event_type", &eventType, "callback", &callback); err != nil {
		return starlark.None, err
	}
	log.Printf("[sysscript events] subscribed to %s (stub)", eventType)
	// TODO: wire eBPF / ETW hook
	return starlark.True, nil
}

// =============================================================================
// sys.packages  — apt/dnf/brew stub
// =============================================================================

func (engine *Engine) packagesInstall(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs("install", args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for packages.install")
	}
	log.Printf("[sysscript packages] install %s (stub)", name)
	// TODO: detect apt/dnf/brew and run appropriate command
	return starlark.True, nil
}

// =============================================================================
// sys.containers  — Docker SDK stub
// =============================================================================

func (engine *Engine) containersRun(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var image string
	if err := starlark.UnpackArgs("run", args, kwargs, "image", &image); err != nil {
		return starlark.None, err
	}
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for containers.run")
	}
	log.Printf("[sysscript containers] run %s (stub)", image)
	// TODO: wire Docker SDK / containerd client
	return starlark.True, nil
}
