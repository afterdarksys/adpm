package sysscript

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"

	gopsnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/disk"
	"go.starlark.net/starlark"
)

// =============================================================================
// sys.disk
//
//   sys.disk.usage(path)     → {total, used, free, percent, fstype, device}
//   sys.disk.partitions()    → list of {device, mountpoint, fstype, opts}
//   sys.disk.io()            → dict of device → {read_bytes, write_bytes, read_count, write_count}
// =============================================================================

func (engine *Engine) diskUsage(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var path string
	if err := starlark.UnpackArgs("usage", args, kwargs, "path", &path); err != nil {
		return starlark.None, err
	}
	u, err := disk.Usage(path)
	if err != nil {
		return starlark.None, fmt.Errorf("disk.usage: %w", err)
	}
	d := starlark.NewDict(6)
	d.SetKey(starlark.String("total"), starlark.MakeUint64(u.Total))
	d.SetKey(starlark.String("used"), starlark.MakeUint64(u.Used))
	d.SetKey(starlark.String("free"), starlark.MakeUint64(u.Free))
	d.SetKey(starlark.String("percent"), starlark.Float(u.UsedPercent))
	d.SetKey(starlark.String("fstype"), starlark.String(u.Fstype))
	d.SetKey(starlark.String("device"), starlark.String(u.Path))
	return d, nil
}

func (engine *Engine) diskPartitions(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	parts, err := disk.Partitions(true)
	if err != nil {
		return starlark.None, fmt.Errorf("disk.partitions: %w", err)
	}
	results := make([]starlark.Value, 0, len(parts))
	for _, p := range parts {
		d := starlark.NewDict(4)
		d.SetKey(starlark.String("device"), starlark.String(p.Device))
		d.SetKey(starlark.String("mountpoint"), starlark.String(p.Mountpoint))
		d.SetKey(starlark.String("fstype"), starlark.String(p.Fstype))
		d.SetKey(starlark.String("opts"), starlark.String(strings.Join(p.Opts, ",")))
		results = append(results, d)
	}
	return starlark.NewList(results), nil
}

func (engine *Engine) diskIO(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	counters, err := disk.IOCounters()
	if err != nil {
		return starlark.None, fmt.Errorf("disk.io: %w", err)
	}
	result := starlark.NewDict(len(counters))
	for name, c := range counters {
		d := starlark.NewDict(4)
		d.SetKey(starlark.String("read_bytes"), starlark.MakeUint64(c.ReadBytes))
		d.SetKey(starlark.String("write_bytes"), starlark.MakeUint64(c.WriteBytes))
		d.SetKey(starlark.String("read_count"), starlark.MakeUint64(c.ReadCount))
		d.SetKey(starlark.String("write_count"), starlark.MakeUint64(c.WriteCount))
		result.SetKey(starlark.String(name), d)
	}
	return result, nil
}

// =============================================================================
// sys.network
//
//   sys.network.interfaces()   → list of {name, addrs, mtu, flags, mac}
//   sys.network.io()           → dict of iface → {bytes_sent, bytes_recv, packets_sent, packets_recv, errin, errout}
//   sys.network.connections()  → list of {fd, family, type, laddr, raddr, status, pid}
//   sys.network.routes()       → list of {dest, gateway, iface, metric} (via ip route)
// =============================================================================

func (engine *Engine) networkInterfaces(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	ifaces, err := gopsnet.Interfaces()
	if err != nil {
		return starlark.None, fmt.Errorf("network.interfaces: %w", err)
	}
	results := make([]starlark.Value, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs := make([]starlark.Value, 0, len(iface.Addrs))
		for _, a := range iface.Addrs {
			addrs = append(addrs, starlark.String(a.Addr))
		}
		flags := make([]starlark.Value, 0, len(iface.Flags))
		for _, f := range iface.Flags {
			flags = append(flags, starlark.String(f))
		}
		d := starlark.NewDict(5)
		d.SetKey(starlark.String("name"), starlark.String(iface.Name))
		d.SetKey(starlark.String("addrs"), starlark.NewList(addrs))
		d.SetKey(starlark.String("mtu"), starlark.MakeInt(iface.MTU))
		d.SetKey(starlark.String("flags"), starlark.NewList(flags))
		d.SetKey(starlark.String("mac"), starlark.String(iface.HardwareAddr))
		results = append(results, d)
	}
	return starlark.NewList(results), nil
}

func (engine *Engine) networkIO(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	counters, err := gopsnet.IOCounters(true)
	if err != nil {
		return starlark.None, fmt.Errorf("network.io: %w", err)
	}
	result := starlark.NewDict(len(counters))
	for _, c := range counters {
		d := starlark.NewDict(6)
		d.SetKey(starlark.String("bytes_sent"), starlark.MakeUint64(c.BytesSent))
		d.SetKey(starlark.String("bytes_recv"), starlark.MakeUint64(c.BytesRecv))
		d.SetKey(starlark.String("packets_sent"), starlark.MakeUint64(c.PacketsSent))
		d.SetKey(starlark.String("packets_recv"), starlark.MakeUint64(c.PacketsRecv))
		d.SetKey(starlark.String("errin"), starlark.MakeUint64(c.Errin))
		d.SetKey(starlark.String("errout"), starlark.MakeUint64(c.Errout))
		result.SetKey(starlark.String(c.Name), d)
	}
	return result, nil
}

func (engine *Engine) networkConnections(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	conns, err := gopsnet.Connections("all")
	if err != nil {
		return starlark.None, fmt.Errorf("network.connections: %w", err)
	}
	results := make([]starlark.Value, 0, len(conns))
	for _, c := range conns {
		d := starlark.NewDict(7)
		d.SetKey(starlark.String("fd"), starlark.MakeInt(int(c.Fd)))
		d.SetKey(starlark.String("family"), starlark.MakeInt(int(c.Family)))
		d.SetKey(starlark.String("type"), starlark.MakeInt(int(c.Type)))
		d.SetKey(starlark.String("laddr"), starlark.String(net.JoinHostPort(c.Laddr.IP, strconv.Itoa(int(c.Laddr.Port)))))
		d.SetKey(starlark.String("raddr"), starlark.String(net.JoinHostPort(c.Raddr.IP, strconv.Itoa(int(c.Raddr.Port)))))
		d.SetKey(starlark.String("status"), starlark.String(c.Status))
		d.SetKey(starlark.String("pid"), starlark.MakeInt(int(c.Pid)))
		results = append(results, d)
	}
	return starlark.NewList(results), nil
}

// sys.network.routes() — parses `ip route` output (Linux) or `netstat -rn` (macOS/BSD)
func (engine *Engine) networkRoutes(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	// Try `ip route` first (Linux), fall back to `netstat -rn` (macOS)
	out, err := exec.Command("ip", "route").Output()
	if err != nil {
		out, err = exec.Command("netstat", "-rn").Output()
		if err != nil {
			return starlark.None, fmt.Errorf("network.routes: neither 'ip route' nor 'netstat -rn' available")
		}
		return parseNetstatRoutes(out), nil
	}
	return parseIPRoutes(out), nil
}

func parseIPRoutes(out []byte) starlark.Value {
	var results []starlark.Value
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		d := starlark.NewDict(4)
		d.SetKey(starlark.String("dest"), starlark.String(fields[0]))
		gw, iface, metric := "", "", ""
		for i, f := range fields {
			switch f {
			case "via":
				if i+1 < len(fields) {
					gw = fields[i+1]
				}
			case "dev":
				if i+1 < len(fields) {
					iface = fields[i+1]
				}
			case "metric":
				if i+1 < len(fields) {
					metric = fields[i+1]
				}
			}
		}
		d.SetKey(starlark.String("gateway"), starlark.String(gw))
		d.SetKey(starlark.String("iface"), starlark.String(iface))
		d.SetKey(starlark.String("metric"), starlark.String(metric))
		results = append(results, d)
	}
	return starlark.NewList(results)
}

func parseNetstatRoutes(out []byte) starlark.Value {
	var results []starlark.Value
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		// netstat -rn: Destination Gateway Flags Refs Use Netif Expire
		if len(fields) < 6 || fields[0] == "Destination" || fields[0] == "Internet" || fields[0] == "Internet6" {
			continue
		}
		d := starlark.NewDict(4)
		d.SetKey(starlark.String("dest"), starlark.String(fields[0]))
		d.SetKey(starlark.String("gateway"), starlark.String(fields[1]))
		d.SetKey(starlark.String("iface"), starlark.String(fields[5]))
		d.SetKey(starlark.String("metric"), starlark.String(""))
		results = append(results, d)
	}
	return starlark.NewList(results)
}
