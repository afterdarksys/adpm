package sysscript

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"go.starlark.net/starlark"
)

// =============================================================================
// sys.accounts — user and group management
//
//   sys.accounts.list_users()                              → list of {username, uid, gid, home, shell, gecos}
//   sys.accounts.list_groups()                             → list of {name, gid, members}
//   sys.accounts.add_user(name, home="", shell="/bin/bash", system=False) → {success, error}
//   sys.accounts.del_user(name, remove_home=False)         → {success, error}
//   sys.accounts.add_group(name, system=False)             → {success, error}
//   sys.accounts.del_group(name)                           → {success, error}
//   sys.accounts.set_password(name, password)              → {success, error}
//   sys.accounts.id(name)                                  → {uid, gid, groups}
//
// add_user/del_user/add_group/del_group/set_password require AllowExec.
// list_users/list_groups/id read /etc/passwd and /etc/group directly.
// =============================================================================

func (engine *Engine) accountsListUsers(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return starlark.None, fmt.Errorf("accounts.list_users: %w", err)
	}
	var results []starlark.Value
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 7)
		if len(parts) < 7 {
			continue
		}
		d := starlark.NewDict(6)
		d.SetKey(starlark.String("username"), starlark.String(parts[0]))
		d.SetKey(starlark.String("uid"), starlark.String(parts[2]))
		d.SetKey(starlark.String("gid"), starlark.String(parts[3]))
		d.SetKey(starlark.String("gecos"), starlark.String(parts[4]))
		d.SetKey(starlark.String("home"), starlark.String(parts[5]))
		d.SetKey(starlark.String("shell"), starlark.String(parts[6]))
		results = append(results, d)
	}
	return starlark.NewList(results), nil
}

func (engine *Engine) accountsListGroups(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	data, err := os.ReadFile("/etc/group")
	if err != nil {
		return starlark.None, fmt.Errorf("accounts.list_groups: %w", err)
	}
	var results []starlark.Value
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 4)
		if len(parts) < 4 {
			continue
		}
		var members []starlark.Value
		for _, m := range strings.Split(parts[3], ",") {
			m = strings.TrimSpace(m)
			if m != "" {
				members = append(members, starlark.String(m))
			}
		}
		d := starlark.NewDict(3)
		d.SetKey(starlark.String("name"), starlark.String(parts[0]))
		d.SetKey(starlark.String("gid"), starlark.String(parts[2]))
		d.SetKey(starlark.String("members"), starlark.NewList(members))
		results = append(results, d)
	}
	return starlark.NewList(results), nil
}

func (engine *Engine) accountsAddUser(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for accounts.add_user")
	}
	var name, home, shell string
	var system bool
	if err := starlark.UnpackArgs("add_user", args, kwargs,
		"name", &name, "home?", &home, "shell?", &shell, "system?", &system,
	); err != nil {
		return starlark.None, err
	}
	if shell == "" {
		shell = "/bin/bash"
	}
	cmdArgs := []string{name, "--shell", shell}
	if home != "" {
		cmdArgs = append(cmdArgs, "--home", home, "--create-home")
	}
	if system {
		cmdArgs = append(cmdArgs, "--system")
	}
	return execResult(exec.Command("useradd", cmdArgs...))
}

func (engine *Engine) accountsDelUser(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for accounts.del_user")
	}
	var name string
	var removeHome bool
	if err := starlark.UnpackArgs("del_user", args, kwargs, "name", &name, "remove_home?", &removeHome); err != nil {
		return starlark.None, err
	}
	cmdArgs := []string{name}
	if removeHome {
		cmdArgs = append([]string{"-r"}, cmdArgs...)
	}
	return execResult(exec.Command("userdel", cmdArgs...))
}

func (engine *Engine) accountsAddGroup(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for accounts.add_group")
	}
	var name string
	var system bool
	if err := starlark.UnpackArgs("add_group", args, kwargs, "name", &name, "system?", &system); err != nil {
		return starlark.None, err
	}
	cmdArgs := []string{name}
	if system {
		cmdArgs = append([]string{"--system"}, cmdArgs...)
	}
	return execResult(exec.Command("groupadd", cmdArgs...))
}

func (engine *Engine) accountsDelGroup(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for accounts.del_group")
	}
	var name string
	if err := starlark.UnpackArgs("del_group", args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	return execResult(exec.Command("groupdel", name))
}

func (engine *Engine) accountsSetPassword(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for accounts.set_password")
	}
	var name, password string
	if err := starlark.UnpackArgs("set_password", args, kwargs, "name", &name, "password", &password); err != nil {
		return starlark.None, err
	}
	// echo "name:password" | chpasswd
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", name, password))
	return execResult(cmd)
}

func (engine *Engine) accountsID(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs("id", args, kwargs, "name", &name); err != nil {
		return starlark.None, err
	}
	out, err := exec.Command("id", name).Output()
	if err != nil {
		return starlark.None, fmt.Errorf("accounts.id: %w", err)
	}
	// parse: uid=1000(user) gid=1000(group) groups=1000(group),27(sudo)
	line := strings.TrimSpace(string(out))
	d := starlark.NewDict(3)
	d.SetKey(starlark.String("raw"), starlark.String(line))
	for _, field := range strings.Fields(line) {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) == 2 {
			d.SetKey(starlark.String(parts[0]), starlark.String(parts[1]))
		}
	}
	return d, nil
}

// =============================================================================
// sys.cron — crontab management
//
//   sys.cron.list(user="")              → list of {schedule, command, raw}
//   sys.cron.add(schedule, command, user="")  → {success, error}
//   sys.cron.remove(command, user="")   → {success, error}  (removes lines containing command)
//   sys.cron.write(content, user="")    → {success, error}  (replace entire crontab)
// =============================================================================

func (engine *Engine) cronList(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var user string
	if err := starlark.UnpackArgs("list", args, kwargs, "user?", &user); err != nil {
		return starlark.None, err
	}
	cmdArgs := []string{"-l"}
	if user != "" {
		cmdArgs = append([]string{"-u", user}, cmdArgs...)
	}
	out, err := exec.Command("crontab", cmdArgs...).Output()
	if err != nil {
		// exit code 1 with "no crontab" is normal
		return starlark.NewList(nil), nil
	}
	var results []starlark.Value
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// standard cron: min hour dom mon dow command (5 fields + rest)
		fields := strings.Fields(line)
		d := starlark.NewDict(3)
		d.SetKey(starlark.String("raw"), starlark.String(line))
		if len(fields) >= 6 {
			d.SetKey(starlark.String("schedule"), starlark.String(strings.Join(fields[:5], " ")))
			d.SetKey(starlark.String("command"), starlark.String(strings.Join(fields[5:], " ")))
		} else {
			d.SetKey(starlark.String("schedule"), starlark.String(""))
			d.SetKey(starlark.String("command"), starlark.String(line))
		}
		results = append(results, d)
	}
	return starlark.NewList(results), nil
}

func (engine *Engine) cronAdd(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for cron.add")
	}
	var schedule, command, user string
	if err := starlark.UnpackArgs("add", args, kwargs, "schedule", &schedule, "command", &command, "user?", &user); err != nil {
		return starlark.None, err
	}
	existing, _ := cronGetRaw(user)
	newLine := fmt.Sprintf("%s %s", schedule, command)
	updated := existing + "\n" + newLine + "\n"
	return cronWrite(updated, user)
}

func (engine *Engine) cronRemove(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for cron.remove")
	}
	var command, user string
	if err := starlark.UnpackArgs("remove", args, kwargs, "command", &command, "user?", &user); err != nil {
		return starlark.None, err
	}
	existing, _ := cronGetRaw(user)
	var kept []string
	for _, line := range strings.Split(existing, "\n") {
		if !strings.Contains(line, command) {
			kept = append(kept, line)
		}
	}
	return cronWrite(strings.Join(kept, "\n")+"\n", user)
}

func (engine *Engine) cronWrite(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if !engine.Entitlements.AllowExec {
		return starlark.None, fmt.Errorf("security exception: exec entitlement required for cron.write")
	}
	var content, user string
	if err := starlark.UnpackArgs("write", args, kwargs, "content", &content, "user?", &user); err != nil {
		return starlark.None, err
	}
	return cronWrite(content, user)
}

func cronGetRaw(user string) (string, error) {
	cmdArgs := []string{"-l"}
	if user != "" {
		cmdArgs = append([]string{"-u", user}, cmdArgs...)
	}
	out, err := exec.Command("crontab", cmdArgs...).Output()
	if err != nil {
		return "", nil // no crontab is fine
	}
	return string(out), nil
}

func cronWrite(content, user string) (starlark.Value, error) {
	cmdArgs := []string{}
	if user != "" {
		cmdArgs = append(cmdArgs, "-u", user)
	}
	cmdArgs = append(cmdArgs, "-")
	cmd := exec.Command("crontab", cmdArgs...)
	cmd.Stdin = strings.NewReader(content)
	return execResult(cmd)
}

// --- shared exec helper ---

func execResult(cmd *exec.Cmd) (starlark.Value, error) {
	out, err := cmd.CombinedOutput()
	d := starlark.NewDict(3)
	if err != nil {
		d.SetKey(starlark.String("success"), starlark.False)
		d.SetKey(starlark.String("output"), starlark.String(strings.TrimSpace(string(out))))
		d.SetKey(starlark.String("error"), starlark.String(err.Error()))
	} else {
		d.SetKey(starlark.String("success"), starlark.True)
		d.SetKey(starlark.String("output"), starlark.String(strings.TrimSpace(string(out))))
		d.SetKey(starlark.String("error"), starlark.String(""))
	}
	return d, nil
}
