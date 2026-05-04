package sysscript

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"go.starlark.net/starlark"
	"gopkg.in/yaml.v3"
)

// --- sys.config.write(path, content) ---

func (engine *Engine) configWrite(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, content string
	if err := starlark.UnpackArgs("write", args, kwargs, "path", &path, "content", &content); err != nil {
		return starlark.None, err
	}
	allowed := false
	for _, prefix := range engine.Entitlements.AllowConfigWrite {
		if prefix == "*" || strings.HasPrefix(path, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		d := starlark.NewDict(2)
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("error"), starlark.String(fmt.Sprintf("security exception: config write to %s not granted", path)))
		return d, nil
	}
	err := engine.cm.AtomicWrite(path, content)
	d := starlark.NewDict(2)
	if err != nil {
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("error"), starlark.String(err.Error()))
	} else {
		d.SetKey(starlark.String("success"), starlark.True)
		d.SetKey(starlark.String("error"), starlark.String(""))
	}
	return d, nil
}

// --- sys.config.template(name, vars) ---

func (engine *Engine) configTemplate(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	var varsVal starlark.Value
	if err := starlark.UnpackArgs("template", args, kwargs, "name", &name, "vars", &varsVal); err != nil {
		return starlark.None, err
	}
	goVars := make(map[string]interface{})
	if d, ok := varsVal.(*starlark.Dict); ok {
		for _, kv := range d.Items() {
			if key, ok := kv[0].(starlark.String); ok {
				goVars[string(key)] = starlarkToGo(kv[1])
			}
		}
	}
	if _, exists := goVars["generated_at"]; !exists {
		goVars["generated_at"] = time.Now().UTC().Format(time.RFC3339)
	}
	rendered, err := engine.cm.RenderTemplate(name, goVars)
	if err != nil {
		return starlark.None, err
	}
	return starlark.String(rendered), nil
}

// --- sys.config.validate(path, service) ---

func (engine *Engine) configValidate(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path, service string
	if err := starlark.UnpackArgs("validate", args, kwargs, "path", &path, "service", &service); err != nil {
		return starlark.None, err
	}
	var cmd *exec.Cmd
	switch strings.ToLower(service) {
	case "postfix":
		cmd = exec.Command("postfix", "check")
	case "bind", "named":
		cmd = exec.Command("named", "-checkconf", path)
	case "nsd":
		cmd = exec.Command("nsd-checkconf", path)
	case "unbound":
		cmd = exec.Command("unbound-checkconf", path)
	default:
		d := starlark.NewDict(3)
		d.SetKey(starlark.String("success"), starlark.True)
		d.SetKey(starlark.String("output"), starlark.String("no validator for service"))
		d.SetKey(starlark.String("error"), starlark.String(""))
		return d, nil
	}
	out, err := cmd.CombinedOutput()
	d := starlark.NewDict(3)
	if err != nil {
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("output"), starlark.String(string(out)))
		d.SetKey(starlark.String("error"), starlark.String(err.Error()))
	} else {
		d.SetKey(starlark.String("success"), starlark.True)
		d.SetKey(starlark.String("output"), starlark.String(string(out)))
		d.SetKey(starlark.String("error"), starlark.String(""))
	}
	return d, nil
}

// --- sys.config.reload(service) ---

func (engine *Engine) configReload(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var service string
	if err := starlark.UnpackArgs("reload", args, kwargs, "service", &service); err != nil {
		return starlark.None, err
	}
	allowed := false
	for _, s := range engine.Entitlements.AllowServiceReload {
		if s == "*" || s == service {
			allowed = true
			break
		}
	}
	if !allowed {
		d := starlark.NewDict(3)
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("output"), starlark.String(""))
		d.SetKey(starlark.String("error"), starlark.String(fmt.Sprintf("security exception: reload of %s not granted", service)))
		return d, nil
	}
	out, err := exec.Command("systemctl", "reload", service).CombinedOutput()
	d := starlark.NewDict(3)
	if err != nil {
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("output"), starlark.String(string(out)))
		d.SetKey(starlark.String("error"), starlark.String(err.Error()))
	} else {
		d.SetKey(starlark.String("success"), starlark.True)
		d.SetKey(starlark.String("output"), starlark.String(string(out)))
		d.SetKey(starlark.String("error"), starlark.String(""))
	}
	return d, nil
}

// --- sys.config.backup(path) ---

func (engine *Engine) configBackup(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("backup", args, kwargs, "path", &path); err != nil {
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
		d := starlark.NewDict(3)
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("backup_path"), starlark.String(""))
		d.SetKey(starlark.String("error"), starlark.String(fmt.Sprintf("security exception: read of %s not granted", path)))
		return d, nil
	}
	backupPath, err := engine.cm.BackupConfig(path)
	d := starlark.NewDict(3)
	if err != nil {
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("backup_path"), starlark.String(""))
		d.SetKey(starlark.String("error"), starlark.String(err.Error()))
	} else {
		d.SetKey(starlark.String("success"), starlark.True)
		d.SetKey(starlark.String("backup_path"), starlark.String(backupPath))
		d.SetKey(starlark.String("error"), starlark.String(""))
	}
	return d, nil
}

// --- sys.config.restore(path) ---

func (engine *Engine) configRestore(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("restore", args, kwargs, "path", &path); err != nil {
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
		for _, prefix := range engine.Entitlements.AllowConfigWrite {
			if prefix == "*" || strings.HasPrefix(path, prefix) {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		d := starlark.NewDict(2)
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("error"), starlark.String(fmt.Sprintf("security exception: write to %s not granted", path)))
		return d, nil
	}
	err := engine.cm.RestoreConfig(path)
	d := starlark.NewDict(2)
	if err != nil {
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("error"), starlark.String(err.Error()))
	} else {
		d.SetKey(starlark.String("success"), starlark.True)
		d.SetKey(starlark.String("error"), starlark.String(""))
	}
	return d, nil
}

// --- sys.yaml.parse(content) ---

func (engine *Engine) yamlParse(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var content string
	if err := starlark.UnpackArgs("parse", args, kwargs, "content", &content); err != nil {
		return starlark.None, err
	}
	var raw interface{}
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return starlark.None, fmt.Errorf("yaml.parse: %w", err)
	}
	return goToStarlark(raw), nil
}

// --- sys.yaml.encode(value) ---

func (engine *Engine) yamlEncode(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val starlark.Value
	if err := starlark.UnpackArgs("encode", args, kwargs, "value", &val); err != nil {
		return starlark.None, err
	}
	out, err := yaml.Marshal(starlarkToGo(val))
	if err != nil {
		return starlark.None, fmt.Errorf("yaml.encode: %w", err)
	}
	return starlark.String(string(out)), nil
}

// --- Conversion helpers ---

func starlarkToGo(v starlark.Value) interface{} {
	switch val := v.(type) {
	case starlark.NoneType:
		return nil
	case starlark.Bool:
		return bool(val)
	case starlark.Int:
		i, _ := val.Int64()
		return i
	case starlark.Float:
		return float64(val)
	case starlark.String:
		return string(val)
	case *starlark.List:
		result := make([]interface{}, val.Len())
		for i := 0; i < val.Len(); i++ {
			result[i] = starlarkToGo(val.Index(i))
		}
		return result
	case *starlark.Dict:
		result := make(map[string]interface{})
		for _, kv := range val.Items() {
			key, ok := kv[0].(starlark.String)
			if !ok {
				result[kv[0].String()] = starlarkToGo(kv[1])
				continue
			}
			result[string(key)] = starlarkToGo(kv[1])
		}
		return result
	default:
		return v.String()
	}
}

func goToStarlark(v interface{}) starlark.Value {
	if v == nil {
		return starlark.None
	}
	switch val := v.(type) {
	case bool:
		return starlark.Bool(val)
	case int:
		return starlark.MakeInt(val)
	case int64:
		return starlark.MakeInt64(val)
	case float64:
		return starlark.Float(val)
	case string:
		return starlark.String(val)
	case []interface{}:
		elems := make([]starlark.Value, len(val))
		for i, item := range val {
			elems[i] = goToStarlark(item)
		}
		return starlark.NewList(elems)
	case map[string]interface{}:
		d := starlark.NewDict(len(val))
		for k, item := range val {
			d.SetKey(starlark.String(k), goToStarlark(item))
		}
		return d
	case map[interface{}]interface{}:
		d := starlark.NewDict(len(val))
		for k, item := range val {
			d.SetKey(starlark.String(fmt.Sprintf("%v", k)), goToStarlark(item))
		}
		return d
	default:
		return starlark.String(fmt.Sprintf("%v", val))
	}
}
