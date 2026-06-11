// Package sensors collects a point-in-time hardware health snapshot straight
// from the Linux kernel's sysfs (hwmon / thermal / power_supply / class/net),
// the same data sources hwcheck.sh reads. It needs no lm_sensors or nvidia-smi,
// and uses smartctl / lspci as a bonus when present.
package sensors

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Status is a per-component verdict.
type Status int

const (
	OK Status = iota
	WRN
	KO
	NA
)

func (s Status) String() string {
	switch s {
	case OK:
		return "OK"
	case WRN:
		return "WRN"
	case KO:
		return "KO"
	default:
		return "NA"
	}
}

// Temp is an optional temperature reading with its safe gradient range.
type Temp struct {
	C   float64
	Min float64
	Max float64
}

// Report is a full hardware snapshot.
type Report struct {
	Host string
	Time time.Time
	Root bool

	CPU         CPUInfo
	RAM         RAMInfo
	GPUs        []GPUInfo
	Disks       []DiskInfo
	Filesystems []FSInfo
	Battery     *BatteryInfo
	Fans        FanInfo
	NICs        []NICInfo
	Audio       AudioInfo

	Load1 float64
}

type CPUInfo struct {
	Model   string
	Logical int
	Cores   int
	Sockets int
	Offline string
	Temp    *Temp
	Status  Status
	Reason  string
}

type RAMInfo struct {
	TotalGiB  float64
	UsedGiB   float64
	UsedPct   int
	SwapTotal float64
	SwapPct   int
	HasSwap   bool
	Temp      *Temp
	DIMMs     []DIMMInfo
	Status    Status
	Reason    string
}

type DIMMInfo struct {
	Size, Type, Speed, Part string
}

type GPUInfo struct {
	Name    string
	Driver  string
	Enabled string
	Temp    *Temp
	Status  Status
	Reason  string
}

type DiskInfo struct {
	Dev    string
	Model  string
	Size   string
	Temp   *Temp
	Status Status // SMART verdict (NA without root/smartctl)
	Reason string
}

type FSInfo struct {
	Mount   string
	UsedPct int
	Size    string
	Flag    string
}

type BatteryInfo struct {
	Name    string // model name (used as the short label)
	Full    string // manufacturer + model (used in the full table)
	Charge  int
	State   string
	WearPct int
	HasWear bool
	Cycles  int
	Temp    *Temp
	Status  Status
	Reason  string
}

type FanInfo struct {
	Fans     []Fan
	Spinning int
	Status   Status
	Reason   string
}

type Fan struct {
	Label string
	RPM   int
	Max   int
}

type NICInfo struct {
	Iface  string
	Kind   string // wired / wireless
	Name   string
	Link   string // up / no-carrier / admin-down / ...
	Addrs  string
	Speed  string
	Temp   *Temp
	Status Status
	Reason string
}

// AudioInfo is the default playback (speaker) and capture (mic) endpoints as
// reported by the PipeWire/PulseAudio server via pactl. Nil endpoints mean the
// server (or pactl) was not reachable.
type AudioInfo struct {
	Speaker *AudioEndpoint
	Mic     *AudioEndpoint
}

// AudioEndpoint is one default audio device's user-facing state. Note this is
// the PipeWire view: it reflects the software mute/volume, not the codec's
// hardware amp/capture switch (which can be muted even while this shows 100%).
type AudioEndpoint struct {
	Kind      string // "Speaker" / "Microphone"
	Name      string // friendly description, e.g. "Arrow Lake cAVS Speaker"
	Detected  bool   // a real default endpoint exists (not dummy/auto_null)
	Muted     bool
	HasVolume bool
	VolumePct int
	Status    Status
	Reason    string
}

// Collect gathers a full snapshot. SMART disk health and DIMM details require
// root (and smartctl / dmidecode); they degrade to NA otherwise.
func Collect() Report {
	host, _ := os.Hostname()
	r := Report{
		Host: host,
		Time: time.Now(),
		Root: os.Geteuid() == 0,
	}
	r.Load1 = loadAvg1()
	r.CPU = collectCPU()
	r.RAM = collectRAM(r.Root)
	r.GPUs = collectGPUs()
	r.Disks = collectDisks(r.Root)
	r.Filesystems = collectFilesystems()
	r.Battery = collectBattery()
	r.Fans = collectFans(r.CPU.Temp)
	r.NICs = collectNICs()
	r.Audio = collectAudio()
	return r
}

// Worst returns the most severe verdict across all components (NA ignored).
func (r Report) Worst() Status {
	worst := OK
	bump := func(s Status) {
		if s == WRN && worst < WRN {
			worst = WRN
		}
		if s == KO {
			worst = KO
		}
	}
	bump(r.CPU.Status)
	bump(r.RAM.Status)
	for _, g := range r.GPUs {
		bump(g.Status)
	}
	for _, d := range r.Disks {
		bump(d.Status)
	}
	if r.Battery != nil {
		bump(r.Battery.Status)
	}
	bump(r.Fans.Status)
	for _, n := range r.NICs {
		bump(n.Status)
	}
	if r.Audio.Speaker != nil {
		bump(r.Audio.Speaker.Status)
	}
	if r.Audio.Mic != nil {
		bump(r.Audio.Mic.Status)
	}
	return worst
}

// Metrics extracts the numeric time-series values for the daemon. Only present
// readings are included (absent sensors are simply omitted).
func (r Report) Metrics() map[string]float64 {
	m := map[string]float64{}
	if r.CPU.Temp != nil {
		m["cpu_temp"] = r.CPU.Temp.C
	}
	// GPU temp: hottest GPU that reports one.
	for _, g := range r.GPUs {
		if g.Temp != nil {
			if cur, ok := m["gpu_temp"]; !ok || g.Temp.C > cur {
				m["gpu_temp"] = g.Temp.C
			}
		}
	}
	if r.RAM.TotalGiB > 0 {
		m["ram_used"] = float64(r.RAM.UsedPct)
	}
	if r.RAM.Temp != nil {
		m["ram_temp"] = r.RAM.Temp.C
	}
	for _, d := range r.Disks {
		if d.Temp != nil {
			if cur, ok := m["disk_temp"]; !ok || d.Temp.C > cur {
				m["disk_temp"] = d.Temp.C
			}
		}
	}
	for _, n := range r.NICs {
		if n.Temp != nil {
			if cur, ok := m["wifi_temp"]; !ok || n.Temp.C > cur {
				m["wifi_temp"] = n.Temp.C
			}
		}
	}
	if r.Battery != nil {
		if r.Battery.Temp != nil {
			m["battery_temp"] = r.Battery.Temp.C
		}
		m["battery_charge"] = float64(r.Battery.Charge)
	}
	if r.Fans.Spinning > 0 {
		max := 0
		for _, f := range r.Fans.Fans {
			if f.RPM > max {
				max = f.RPM
			}
		}
		m["fan_rpm"] = float64(max)
	}
	m["load1"] = r.Load1
	if r.Audio.Speaker != nil && r.Audio.Speaker.HasVolume {
		m["speaker_volume"] = float64(r.Audio.Speaker.VolumePct)
	}
	if r.Audio.Mic != nil && r.Audio.Mic.HasVolume {
		m["mic_volume"] = float64(r.Audio.Mic.VolumePct)
	}
	return m
}

// --------------------------------------------------------------------------
// CPU
// --------------------------------------------------------------------------

func collectCPU() CPUInfo {
	c := CPUInfo{}
	physical := map[string]bool{}
	if f, err := os.Open("/proc/cpuinfo"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			k, v, ok := splitKV(line)
			if !ok {
				continue
			}
			switch k {
			case "model name":
				if c.Model == "" {
					c.Model = v
				}
				c.Logical++
			case "cpu cores":
				if n, e := strconv.Atoi(v); e == nil {
					c.Cores = n
				}
			case "physical id":
				physical[v] = true
			}
		}
		f.Close()
	}
	c.Sockets = len(physical)
	c.Offline = readStr("/sys/devices/system/cpu/offline")

	if t, ok := hwmonMaxTempByName("coretemp", "k10temp", "zenpower"); ok {
		c.Temp = &Temp{C: t, Min: 35, Max: 95}
	} else if t, ok := thermalZoneTemp("x86_pkg_temp", "tcpu", "cpu"); ok {
		c.Temp = &Temp{C: t, Min: 35, Max: 95}
	}

	switch {
	case c.Temp == nil:
		c.Status, c.Reason = NA, "no temperature sensor exposed"
	case c.Temp.C >= 95:
		c.Status, c.Reason = KO, "package temperature critical ("+degC(c.Temp.C)+" >=95)"
	case c.Temp.C >= 85:
		c.Status, c.Reason = WRN, "running hot ("+degC(c.Temp.C)+") — check fans/dust"
	default:
		c.Status, c.Reason = OK, "temperature normal ("+degC(c.Temp.C)+")"
	}
	return c
}

// --------------------------------------------------------------------------
// RAM
// --------------------------------------------------------------------------

func collectRAM(root bool) RAMInfo {
	r := RAMInfo{}
	mi := readMeminfo()
	mt := mi["MemTotal"]
	ma := mi["MemAvailable"]
	if mt > 0 {
		used := mt - ma
		r.UsedPct = int(used * 100 / mt)
		r.TotalGiB = float64(mt) / 1048576.0
		r.UsedGiB = float64(used) / 1048576.0
		if sw := mi["SwapTotal"]; sw > 0 {
			r.HasSwap = true
			r.SwapTotal = float64(sw) / 1048576.0
			r.SwapPct = int((sw - mi["SwapFree"]) * 100 / sw)
		}
	}

	if t, ok := hwmonMaxTempByName("spd5118", "jc42"); ok {
		r.Temp = &Temp{C: t, Min: 30, Max: 90}
	}

	if root {
		r.DIMMs = collectDIMMs()
	}

	switch {
	case mt == 0:
		r.Status, r.Reason = NA, "could not read /proc/meminfo"
	case r.Temp != nil && r.Temp.C >= 90:
		r.Status, r.Reason = KO, "DIMM temperature critical ("+degC(r.Temp.C)+")"
	case r.UsedPct >= 95:
		r.Status, r.Reason = WRN, "memory pressure high ("+strconv.Itoa(r.UsedPct)+"% used)"
	case r.Temp != nil && r.Temp.C >= 80:
		r.Status, r.Reason = WRN, "DIMMs warm ("+degC(r.Temp.C)+")"
	default:
		reason := strconv.Itoa(r.UsedPct) + "% used"
		if r.Temp != nil {
			reason += ", DIMMs " + degC(r.Temp.C)
		}
		r.Status, r.Reason = OK, reason
	}
	return r
}

func collectDIMMs() []DIMMInfo {
	out, err := exec.Command("dmidecode", "-t", "memory").Output()
	if err != nil {
		return nil
	}
	var dimms []DIMMInfo
	var cur DIMMInfo
	inDev := false
	flush := func() {
		if cur.Size != "" && cur.Size != "No" {
			dimms = append(dimms, cur)
		}
		cur = DIMMInfo{}
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "Memory Device"):
			if inDev {
				flush()
			}
			inDev = true
		case !inDev:
			continue
		case strings.HasPrefix(line, "Size:"):
			v := strings.TrimSpace(strings.TrimPrefix(line, "Size:"))
			if v != "" && v[0] >= '0' && v[0] <= '9' {
				cur.Size = v
			}
		case strings.HasPrefix(line, "Type:"):
			v := strings.TrimSpace(strings.TrimPrefix(line, "Type:"))
			if v != "" && !strings.Contains(v, "Unknown") {
				cur.Type = v
			}
		case strings.HasPrefix(line, "Speed:"):
			v := strings.TrimSpace(strings.TrimPrefix(line, "Speed:"))
			if v != "" && v[0] >= '0' && v[0] <= '9' {
				cur.Speed = v
			}
		case strings.HasPrefix(line, "Part Number:"):
			cur.Part = strings.TrimSpace(strings.TrimPrefix(line, "Part Number:"))
		}
	}
	if inDev {
		flush()
	}
	return dimms
}

// --------------------------------------------------------------------------
// GPUs
// --------------------------------------------------------------------------

func collectGPUs() []GPUInfo {
	var gpus []GPUInfo
	names := lspciNames() // slot -> friendly name, best-effort

	devs, _ := filepath.Glob("/sys/bus/pci/devices/*")
	for _, dev := range devs {
		class := readStr(filepath.Join(dev, "class"))
		// 0x0300xx VGA, 0x0302xx 3D, 0x0380xx display
		if !strings.HasPrefix(class, "0x0300") &&
			!strings.HasPrefix(class, "0x0302") &&
			!strings.HasPrefix(class, "0x0380") {
			continue
		}
		vendor := strings.ToLower(readStr(filepath.Join(dev, "vendor")))
		slot := shortSlot(filepath.Base(dev))
		driver := ""
		if l, err := os.Readlink(filepath.Join(dev, "driver")); err == nil {
			driver = filepath.Base(l)
		}

		g := GPUInfo{Driver: driver}
		g.Name = names[slot]
		if g.Name == "" {
			g.Name = vendorName(vendor) + " GPU " + slot
		}

		switch {
		case vendor == "0x10de": // NVIDIA
			if t, ok := nvidiaTemp(); ok {
				g.Temp = &Temp{C: t, Min: 35, Max: 95}
			} else if t, ok := hwmonMaxTempByName("nouveau"); ok {
				g.Temp = &Temp{C: t, Min: 35, Max: 95}
			}
			if driver == "" {
				g.Enabled = "idle/unbound (hybrid)"
			} else {
				g.Enabled = "active"
			}
		case vendor == "0x1002": // AMD
			if t, ok := hwmonMaxTempByName("amdgpu"); ok {
				g.Temp = &Temp{C: t, Min: 35, Max: 95}
			}
			if driver != "" {
				g.Enabled = "active"
			}
		case vendor == "0x8086": // Intel
			if driver != "" {
				g.Enabled = "active"
			}
		default:
			if driver != "" {
				g.Enabled = "active"
			}
		}

		switch {
		case driver != "" && g.Temp != nil && g.Temp.C >= 95:
			g.Status, g.Reason = KO, "GPU temperature critical (>=95°C)"
		case driver != "":
			g.Status, g.Reason = OK, "active and driven by "+driver
		case vendor == "0x10de":
			g.Status, g.Reason = WRN, "dGPU powered down/unbound (normal for hybrid; or driver not installed)"
		default:
			g.Status, g.Reason = WRN, "no driver bound (idle/disabled)"
		}
		gpus = append(gpus, g)
	}
	return gpus
}

func nvidiaTemp() (float64, bool) {
	out, err := exec.Command("nvidia-smi",
		"--query-gpu=temperature.gpu", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, false
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	v, err := strconv.ParseFloat(strings.TrimSpace(line), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// --------------------------------------------------------------------------
// Disks
// --------------------------------------------------------------------------

func collectDisks(root bool) []DiskInfo {
	var disks []DiskInfo
	blocks, _ := filepath.Glob("/sys/block/*")
	for _, b := range blocks {
		name := filepath.Base(b)
		switch {
		case strings.HasPrefix(name, "zram"),
			strings.HasPrefix(name, "loop"),
			strings.HasPrefix(name, "ram"),
			strings.HasPrefix(name, "dm-"),
			strings.HasPrefix(name, "md"),
			strings.HasPrefix(name, "sr"):
			continue
		}
		d := DiskInfo{Dev: "/dev/" + name}
		d.Model = readStr(filepath.Join(b, "device", "model"))
		if d.Model == "" {
			d.Model = name
		}
		if sectors, ok := readInt(filepath.Join(b, "size")); ok {
			d.Size = humanBytes(sectors * 512)
		}

		if strings.HasPrefix(name, "nvme") {
			if t, ok := hwmonMaxTempByName("nvme"); ok {
				d.Temp = &Temp{C: t, Min: 25, Max: 85}
			}
		}
		if d.Temp == nil {
			if t, ok := hwmonMaxTempByName("drivetemp"); ok {
				d.Temp = &Temp{C: t, Min: 25, Max: 85}
			}
		}

		d.Status, d.Reason = smartHealth(d.Dev, root)
		disks = append(disks, d)
	}
	return disks
}

func smartHealth(dev string, root bool) (Status, string) {
	if _, err := exec.LookPath("smartctl"); err != nil {
		return NA, "smartctl not installed"
	}
	if !root {
		return NA, "re-run with sudo for SMART health"
	}
	out, _ := exec.Command("smartctl", "-H", dev).Output()
	s := string(out)
	up := strings.ToUpper(s)
	switch {
	case strings.Contains(up, "PASSED") || strings.Contains(up, "OK"):
		return OK, "SMART self-assessment PASSED"
	case strings.Contains(up, "FAILED") || strings.Contains(up, "FAILING"):
		return KO, "SMART FAILING — back up data NOW"
	default:
		return WRN, "SMART status unclear"
	}
}

// --------------------------------------------------------------------------
// Filesystems
// --------------------------------------------------------------------------

var pseudoFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true,
	"tmpfs": true, "cgroup": true, "cgroup2": true, "pstore": true,
	"bpf": true, "debugfs": true, "tracefs": true, "securityfs": true,
	"mqueue": true, "hugetlbfs": true, "configfs": true, "fusectl": true,
	"autofs": true, "binfmt_misc": true, "ramfs": true, "squashfs": true,
	"overlay": true, "nsfs": true, "rpc_pipefs": true, "efivarfs": true,
}

func collectFilesystems() []FSInfo {
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return nil
	}
	defer f.Close()

	seen := map[string]bool{}
	var out []FSInfo
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 3 {
			continue
		}
		src, mount, fstype := fields[0], fields[1], fields[2]
		if pseudoFS[fstype] || !strings.HasPrefix(src, "/dev/") {
			continue
		}
		if seen[src] {
			continue
		}
		seen[src] = true

		var st syscall.Statfs_t
		if syscall.Statfs(mount, &st) != nil || st.Blocks == 0 {
			continue
		}
		total := st.Blocks * uint64(st.Bsize)
		free := st.Bavail * uint64(st.Bsize)
		usedPct := int((total - free) * 100 / total)
		fi := FSInfo{Mount: mount, UsedPct: usedPct, Size: humanBytesU(total)}
		if usedPct >= 90 {
			fi.Flag = "!! >=90%"
		}
		out = append(out, fi)
	}
	return out
}

// --------------------------------------------------------------------------
// Battery
// --------------------------------------------------------------------------

func collectBattery() *BatteryInfo {
	matches, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	if len(matches) == 0 {
		return nil
	}
	p := matches[0]
	manu := readStr(filepath.Join(p, "manufacturer"))
	model := readStr(filepath.Join(p, "model_name"))
	b := &BatteryInfo{
		Name:  model,
		Full:  strings.TrimSpace(manu + " " + model),
		State: readStr(filepath.Join(p, "status")),
	}
	if b.Name == "" {
		b.Name, b.Full = "BAT", "BAT"
	}
	if v, ok := readInt(filepath.Join(p, "capacity")); ok {
		b.Charge = int(v)
	}
	if v, ok := readInt(filepath.Join(p, "cycle_count")); ok {
		b.Cycles = int(v)
	}

	full, fok := readInt(filepath.Join(p, "charge_full"))
	fulld, dok := readInt(filepath.Join(p, "charge_full_design"))
	if !fok {
		full, fok = readInt(filepath.Join(p, "energy_full"))
	}
	if !dok {
		fulld, dok = readInt(filepath.Join(p, "energy_full_design"))
	}
	if fok && dok && fulld > 0 {
		w := int((fulld - full) * 100 / fulld)
		if w < 0 {
			w = 0
		}
		b.WearPct, b.HasWear = w, true
	}

	if v, ok := readInt(filepath.Join(p, "temp")); ok {
		b.Temp = &Temp{C: float64(v) / 10.0, Min: 15, Max: 60}
	}

	health := strings.ToLower(readStr(filepath.Join(p, "health")))
	switch {
	case health == "dead" || (b.HasWear && b.WearPct >= 50):
		b.Status, b.Reason = KO, "battery heavily degraded ("+itoaOr(b.WearPct, b.HasWear)+"% worn) — consider replacement"
	case b.HasWear && b.WearPct >= 25:
		b.Status, b.Reason = WRN, "noticeable wear ("+strconv.Itoa(b.WearPct)+"% capacity lost, "+strconv.Itoa(b.Cycles)+" cycles)"
	case health != "" && health != "good" && health != "unknown":
		b.Status, b.Reason = WRN, "reported health: "+health
	default:
		reason := ""
		if b.HasWear {
			reason = strconv.Itoa(b.WearPct) + "% worn, "
		}
		b.Status, b.Reason = OK, reason+strconv.Itoa(b.Cycles)+" cycles — healthy"
	}
	return b
}

// --------------------------------------------------------------------------
// Fans
// --------------------------------------------------------------------------

func collectFans(cpuTemp *Temp) FanInfo {
	dir := hwmonDir("dell_smm", "thinkpad")
	if dir == "" {
		// any hwmon exposing a fan tachometer
		for _, h := range mustGlob("/sys/class/hwmon/hwmon*") {
			if fans, _ := filepath.Glob(filepath.Join(h, "fan*_input")); len(fans) > 0 {
				dir = h
				break
			}
		}
	}
	if dir == "" {
		return FanInfo{Status: NA, Reason: "no fan RPM sensor (may need lm_sensors / vendor module)"}
	}

	var fi FanInfo
	inputs, _ := filepath.Glob(filepath.Join(dir, "fan*_input"))
	for i, in := range inputs {
		rpm, ok := readInt(in)
		if !ok {
			continue
		}
		f := Fan{Label: readStr(strings.TrimSuffix(in, "_input") + "_label"), RPM: int(rpm)}
		if f.Label == "" {
			f.Label = "Fan " + strconv.Itoa(i+1)
		}
		if m, ok := readInt(strings.TrimSuffix(in, "_input") + "_max"); ok {
			f.Max = int(m)
		}
		fi.Fans = append(fi.Fans, f)
		if rpm > 0 {
			fi.Spinning++
		}
	}

	switch {
	case len(fi.Fans) == 0:
		fi.Status, fi.Reason = NA, "no readable fan inputs"
	case fi.Spinning > 0:
		fi.Status, fi.Reason = OK, strconv.Itoa(fi.Spinning)+"/"+strconv.Itoa(len(fi.Fans))+" fan(s) spinning"
	case cpuTemp != nil && cpuTemp.C >= 70:
		fi.Status, fi.Reason = KO, "all fans report 0 RPM while CPU is "+degC(cpuTemp.C)+" — possible fan failure"
	default:
		fi.Status, fi.Reason = OK, "fans idle at 0 RPM (CPU cool — normal passive cooling)"
	}
	return fi
}

// --------------------------------------------------------------------------
// Network cards
// --------------------------------------------------------------------------

func collectNICs() []NICInfo {
	var nics []NICInfo
	ifaces, _ := filepath.Glob("/sys/class/net/*")
	for _, ifp := range ifaces {
		iface := filepath.Base(ifp)
		if iface == "lo" {
			continue
		}
		switch {
		case strings.HasPrefix(iface, "docker"),
			strings.HasPrefix(iface, "veth"),
			strings.HasPrefix(iface, "br-"),
			strings.HasPrefix(iface, "virbr"),
			strings.HasPrefix(iface, "tun"),
			strings.HasPrefix(iface, "tap"):
			continue
		}
		n := NICInfo{Iface: iface, Kind: "wired"}
		if _, err := os.Stat(filepath.Join(ifp, "wireless")); err == nil {
			n.Kind = "wireless"
		}

		// friendly name + driver via the device link
		if dev, err := os.Readlink(filepath.Join(ifp, "device")); err == nil {
			pci := filepath.Base(dev)
			n.Name = lspciNames()[shortSlot(pci)]
			if l, err := os.Readlink(filepath.Join(ifp, "device", "driver")); err == nil {
				_ = l
			}
		}
		if n.Name == "" {
			n.Name = "unknown adapter"
		}

		oper := readStr(filepath.Join(ifp, "operstate"))
		flags := readStr(filepath.Join(ifp, "flags"))
		adminUp := false
		if v, err := strconv.ParseInt(strings.TrimPrefix(flags, "0x"), 16, 64); err == nil {
			adminUp = v&0x1 != 0
		}
		n.Addrs = ifaceAddrs(iface)

		if n.Kind == "wireless" {
			if t, ok := hwmonMaxTempByPrefix("iwlwifi", "mt79", "mt76", "ath1", "ath", "wifi"); ok {
				n.Temp = &Temp{C: t, Min: 30, Max: 90}
			}
		}

		switch {
		case !adminUp:
			n.Link, n.Status, n.Reason = "admin-down", WRN, "administratively DOWN (disabled)"
		case oper == "up":
			n.Link, n.Status = "up", OK
			n.Reason = "link up"
			if n.Addrs != "" {
				n.Reason += " — " + n.Addrs
			}
		case oper == "down":
			n.Link = "no-carrier"
			if n.Kind == "wired" {
				n.Status, n.Reason = WRN, "enabled but no carrier (cable unplugged?)"
			} else {
				n.Status, n.Reason = WRN, "enabled but not associated (not connected)"
			}
		case oper == "dormant":
			n.Link, n.Status, n.Reason = "dormant", WRN, "dormant (authenticating)"
		default:
			if oper == "" {
				oper = "unknown"
			}
			n.Link, n.Status, n.Reason = oper, NA, "state: "+oper
		}
		nics = append(nics, n)
	}
	return nics
}

// --------------------------------------------------------------------------
// Audio (speaker + mic) — via pactl (PipeWire/PulseAudio)
// --------------------------------------------------------------------------

func collectAudio() AudioInfo {
	var a AudioInfo
	if _, err := exec.LookPath("pactl"); err != nil {
		return a // no pactl: leave both endpoints nil (NA in the views)
	}
	a.Speaker = pactlEndpoint("Speaker", "sinks", "get-default-sink", "sink")
	a.Mic = pactlEndpoint("Microphone", "sources", "get-default-source", "source")
	return a
}

// pactlEndpoint resolves the default sink/source and its mute + volume by
// parsing `pactl list <listType>` for the block whose Name matches the default.
func pactlEndpoint(kind, listType, defaultCmd, word string) *AudioEndpoint {
	e := &AudioEndpoint{Kind: kind}
	def := strings.TrimSpace(pactlOut(defaultCmd))
	if def == "" || strings.Contains(def, "auto_null") || strings.Contains(def, "dummy") {
		e.Status, e.Reason = WRN, "no real default "+word+" (no audio device / dummy output)"
		return e
	}
	desc, muted, vol, hasVol, found := parsePactlList(pactlOut("list", listType), def)
	if !found {
		e.Status, e.Reason = WRN, "default "+word+" '"+shortNodeName(def)+"' not found in pactl list"
		return e
	}
	e.Detected = true
	e.Name = desc
	if e.Name == "" {
		e.Name = shortNodeName(def)
	}
	e.Muted = muted
	e.VolumePct, e.HasVolume = vol, hasVol
	volStr := "n/a"
	if hasVol {
		volStr = strconv.Itoa(vol) + "%"
	}
	if muted {
		e.Status, e.Reason = OK, "detected, MUTED ("+volStr+")"
	} else {
		e.Status, e.Reason = OK, "detected, unmuted ("+volStr+")"
	}
	return e
}

func pactlOut(args ...string) string {
	out, err := exec.Command("pactl", args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

var pctRE = regexp.MustCompile(`(\d+)%`)

// parsePactlList scans `pactl list sinks|sources` output for the block whose
// "Name:" equals want, returning its Description, mute and volume%.
func parsePactlList(out, want string) (desc string, muted bool, vol int, hasVol, found bool) {
	var (
		curName, curDesc, curMute, curVol string
		inWanted                          bool
	)
	finish := func() {
		if inWanted {
			desc = strings.TrimSpace(curDesc)
			muted = strings.EqualFold(strings.TrimSpace(curMute), "yes")
			if m := pctRE.FindStringSubmatch(curVol); m != nil {
				if v, err := strconv.Atoi(m[1]); err == nil {
					vol, hasVol = v, true
				}
			}
			found = true
		}
	}
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		// A new block header ("Sink #N" / "Source #N") starts at column 0.
		if line != "" && line[0] != ' ' && line[0] != '\t' {
			finish()
			if found {
				return
			}
			curName, curDesc, curMute, curVol, inWanted = "", "", "", "", false
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "Name:"):
			curName = strings.TrimSpace(strings.TrimPrefix(trimmed, "Name:"))
			inWanted = curName == want
		case strings.HasPrefix(trimmed, "Description:"):
			curDesc = strings.TrimSpace(strings.TrimPrefix(trimmed, "Description:"))
		case strings.HasPrefix(trimmed, "Mute:"):
			curMute = strings.TrimSpace(strings.TrimPrefix(trimmed, "Mute:"))
		case strings.HasPrefix(trimmed, "Volume:") && curVol == "":
			curVol = trimmed
		}
	}
	finish()
	return
}

// shortNodeName trims the long ALSA node name to its last dotted segment.
func shortNodeName(s string) string {
	if i := strings.LastIndexByte(s, '.'); i >= 0 && i < len(s)-1 {
		return s[i+1:]
	}
	return s
}

func ifaceAddrs(iface string) string {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return ""
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return ""
	}
	var ss []string
	for _, a := range addrs {
		ss = append(ss, a.String())
	}
	return strings.Join(ss, ",")
}

// --------------------------------------------------------------------------
// small helpers
// --------------------------------------------------------------------------

func splitKV(line string) (string, string, bool) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

func readMeminfo() map[string]int64 {
	m := map[string]int64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		if v, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			m[key] = v // kB
		}
	}
	return m
}

func loadAvg1() float64 {
	s := readStr("/proc/loadavg")
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

// lspciNamesCache memoizes the lspci slot->name map for one process run.
var lspciNamesCache map[string]string

func lspciNames() map[string]string {
	if lspciNamesCache != nil {
		return lspciNamesCache
	}
	lspciNamesCache = map[string]string{}
	out, err := exec.Command("lspci", "-mm").Output()
	if err != nil {
		return lspciNamesCache
	}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		slot, rest, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		// rest is: "Class" "Vendor" "Device" ... ; join Vendor + Device.
		fields := splitQuoted(rest)
		if len(fields) >= 3 {
			lspciNamesCache[shortSlot(slot)] = fields[1] + " " + fields[2]
		}
	}
	return lspciNamesCache
}

// splitQuoted splits a line of `"a" "b" "c"` quoted fields.
func splitQuoted(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, '"')
		if i < 0 {
			break
		}
		s = s[i+1:]
		j := strings.IndexByte(s, '"')
		if j < 0 {
			break
		}
		out = append(out, s[:j])
		s = s[j+1:]
	}
	return out
}

// shortSlot reduces "0000:01:00.0" to "01:00.0" (lspci -mm's default form).
func shortSlot(slot string) string {
	if parts := strings.SplitN(slot, ":", 2); len(parts) == 2 && len(parts[0]) == 4 {
		return parts[1]
	}
	return slot
}

func vendorName(vendor string) string {
	switch vendor {
	case "0x10de":
		return "NVIDIA"
	case "0x1002":
		return "AMD"
	case "0x8086":
		return "Intel"
	default:
		return "PCI"
	}
}

func degC(c float64) string { return strconv.Itoa(int(c+0.5)) + "°C" }

func itoaOr(v int, ok bool) string {
	if !ok {
		return "?"
	}
	return strconv.Itoa(v)
}

func humanBytes(b int64) string { return humanBytesU(uint64(b)) }
func humanBytesU(b uint64) string {
	const unit = 1024
	if b < unit {
		return strconv.FormatUint(b, 10) + "B"
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	val := float64(b) / float64(div)
	units := []string{"K", "M", "G", "T", "P"}
	return strconv.FormatFloat(val, 'f', 1, 64) + units[exp]
}

func mustGlob(pat string) []string {
	m, _ := filepath.Glob(pat)
	return m
}
