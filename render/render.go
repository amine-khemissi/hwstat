// Package render reproduces hwcheck.sh's realtime presentation in Go: one
// compact table per component family (full view) or one OK/WRN/KO line per
// component (synthetic view), with every temperature colored on a green→red
// gradient scaled to that component's own safe min/max range.
package render

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/amine-khemissi/hwstat/sensors"
)

// Options controls presentation.
type Options struct {
	Color     bool
	TrueColor bool
}

// AutoOptions detects color support from the environment (TTY + COLORTERM).
func AutoOptions(noColor bool) Options {
	o := Options{}
	if noColor {
		return o
	}
	if fi, _ := os.Stdout.Stat(); fi != nil && fi.Mode()&os.ModeCharDevice != 0 {
		o.Color = true
		switch os.Getenv("COLORTERM") {
		case "truecolor", "24bit":
			o.TrueColor = true
		}
	}
	return o
}

type R struct {
	o Options
	b *strings.Builder
	c palette
}

type palette struct {
	rst, bold, dim, grn, red, yel, blu string
}

func New(o Options) *R {
	r := &R{o: o, b: &strings.Builder{}}
	if o.Color {
		r.c = palette{"\x1b[0m", "\x1b[1m", "\x1b[2m", "\x1b[32m", "\x1b[31m", "\x1b[33m", "\x1b[36m"}
	}
	return r
}

func (r *R) String() string { return r.b.String() }

// ------------------------------------------------------------------ gradient

// gradFromFrac maps 0..100 along green→red to an ANSI escape.
func (r *R) gradFromFrac(f int) string {
	if f < 0 {
		f = 0
	}
	if f > 100 {
		f = 100
	}
	if r.o.TrueColor {
		var red, grn int
		if f <= 50 {
			red, grn = f*255/50, 255
		} else {
			red, grn = 255, (100-f)*255/50
		}
		return fmt.Sprintf("\x1b[38;2;%d;%d;0m", red, grn)
	}
	if r.o.Color {
		switch {
		case f >= 80:
			return r.c.red
		case f >= 50:
			return r.c.yel
		default:
			return r.c.grn
		}
	}
	return ""
}

func (r *R) gradTemp(t *sensors.Temp) string {
	if t == nil {
		return ""
	}
	f := 0
	if t.Max > t.Min {
		f = int((t.C - t.Min) * 100 / (t.Max - t.Min))
	}
	return r.gradFromFrac(f)
}

// tempCell renders a temperature with gradient color; "—" if absent.
func (r *R) tempCell(t *sensors.Temp) string {
	if t == nil {
		return "—"
	}
	v := strconv.Itoa(int(t.C + 0.5))
	if !r.o.Color {
		return v + "°C"
	}
	return r.gradTemp(t) + v + "°C" + r.c.rst
}

func (r *R) stateCell(s sensors.Status) string {
	if !r.o.Color {
		return s.String()
	}
	var col string
	switch s {
	case sensors.OK:
		col = r.c.grn
	case sensors.KO:
		col = r.c.red
	case sensors.WRN:
		col = r.c.yel
	default:
		col = r.c.dim
	}
	return col + s.String() + r.c.rst
}

// gradBar renders the small green→red legend bar.
func (r *R) gradBar(cols int) string {
	if !r.o.Color {
		return "cool ----> hot"
	}
	var sb strings.Builder
	for i := 0; i < cols; i++ {
		f := 0
		if cols > 1 {
			f = i * 100 / (cols - 1)
		}
		sb.WriteString(r.gradFromFrac(f))
		sb.WriteString("█")
	}
	sb.WriteString(r.c.rst)
	return sb.String()
}

// --------------------------------------------------------------- table engine

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func visLen(s string) int { return len([]rune(ansiRE.ReplaceAllString(s, ""))) }

func padCell(s string, w int) string {
	pad := w - visLen(s)
	if pad < 0 {
		pad = 0
	}
	return s + strings.Repeat(" ", pad)
}

func trunc(s string, w int) string {
	rs := []rune(s)
	if len(rs) > w {
		return string(rs[:w-1]) + "…"
	}
	return s
}

func (r *R) table(headers []string, rows [][]string) {
	width := make([]int, len(headers))
	for i, h := range headers {
		width[i] = visLen(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if i < len(width) && visLen(c) > width[i] {
				width[i] = visLen(c)
			}
		}
	}
	r.b.WriteString("  ")
	for i, h := range headers {
		r.b.WriteString(r.c.bold + r.c.dim + padCell(h, width[i]) + r.c.rst)
		if i < len(headers)-1 {
			r.b.WriteString("  ")
		}
	}
	r.b.WriteByte('\n')
	for _, row := range rows {
		r.b.WriteString("  ")
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			r.b.WriteString(padCell(cell, width[i]))
			if i < len(headers)-1 {
				r.b.WriteString("  ")
			}
		}
		r.b.WriteByte('\n')
	}
}

func (r *R) section(title string) {
	r.b.WriteString(fmt.Sprintf("\n%s%s== %s ==%s\n", r.c.bold, r.c.blu, title, r.c.rst))
}

func (r *R) dimln(format string, a ...any) {
	r.b.WriteString("  " + r.c.dim + fmt.Sprintf(format, a...) + r.c.rst + "\n")
}

// ------------------------------------------------------------------- views

// Full renders the per-family table report.
func (r *R) Full(rep sensors.Report) {
	r.b.WriteString(fmt.Sprintf("%s%sHardware health check%s  —  %s  —  %s\n",
		r.c.bold, r.c.blu, r.c.rst, rep.Host, rep.Time.Format("2006-01-02 15:04:05")))
	if !rep.Root {
		r.dimln("(run with sudo for SMART disk health + DIMM info)")
	}

	// CPU
	r.section("CPU")
	topo := strconv.Itoa(rep.CPU.Logical) + " logical"
	if rep.CPU.Sockets > 0 {
		topo += fmt.Sprintf(" (%ds/%dc)", rep.CPU.Sockets, rep.CPU.Cores)
	}
	if rep.CPU.Offline != "" {
		topo += "; OFFLINE: " + rep.CPU.Offline
	}
	r.table([]string{"MODEL", "TOPOLOGY", "TEMP", "ST", "NOTE"}, [][]string{{
		trunc(orUnknown(rep.CPU.Model), 40), topo, r.tempCell(rep.CPU.Temp),
		r.stateCell(rep.CPU.Status), rep.CPU.Reason,
	}})

	// RAM
	r.section("RAM")
	swap := "none"
	if rep.RAM.HasSwap {
		swap = fmt.Sprintf("%d%% of %.1f GiB", rep.RAM.SwapPct, rep.RAM.SwapTotal)
	}
	r.table([]string{"TOTAL", "USED", "SWAP", "TEMP", "ST", "NOTE"}, [][]string{{
		fmt.Sprintf("%.1f GiB", rep.RAM.TotalGiB),
		fmt.Sprintf("%d%% (%.1f GiB)", rep.RAM.UsedPct, rep.RAM.UsedGiB),
		swap, r.tempCell(rep.RAM.Temp), r.stateCell(rep.RAM.Status), rep.RAM.Reason,
	}})
	if len(rep.RAM.DIMMs) > 0 {
		var rows [][]string
		for _, d := range rep.RAM.DIMMs {
			rows = append(rows, []string{d.Size, d.Type, d.Speed, d.Part})
		}
		r.table([]string{"DIMM SIZE", "TYPE", "SPEED", "PART NUMBER"}, rows)
	}

	// GPUs
	r.section("GPUs")
	if len(rep.GPUs) == 0 {
		r.dimln("(no GPU found)")
	} else {
		var rows [][]string
		for i, g := range rep.GPUs {
			rows = append(rows, []string{
				strconv.Itoa(i + 1), trunc(g.Name, 40), orValue(g.Driver, "none"),
				orValue(g.Enabled, "-"), r.tempCell(g.Temp), r.stateCell(g.Status), g.Reason,
			})
		}
		r.table([]string{"#", "NAME", "DRIVER", "ENABLED", "TEMP", "ST", "NOTE"}, rows)
	}

	// Disks
	r.section("Disks")
	if len(rep.Disks) == 0 {
		r.dimln("(no disk found)")
	} else {
		var rows [][]string
		for _, d := range rep.Disks {
			rows = append(rows, []string{
				d.Dev, trunc(d.Model, 32), d.Size, r.tempCell(d.Temp),
				r.stateCell(d.Status), d.Reason,
			})
		}
		r.table([]string{"DEVICE", "MODEL", "SIZE", "TEMP", "ST", "SMART"}, rows)
	}

	// Filesystems
	r.section("Filesystems")
	if len(rep.Filesystems) == 0 {
		r.dimln("(no filesystem found)")
	} else {
		var rows [][]string
		for _, f := range rep.Filesystems {
			rows = append(rows, []string{trunc(f.Mount, 34), strconv.Itoa(f.UsedPct) + "%", f.Size, f.Flag})
		}
		r.table([]string{"MOUNT", "USED", "SIZE", "FLAG"}, rows)
	}

	// Battery
	r.section("Battery")
	if rep.Battery == nil {
		r.dimln("(no battery — desktop?)")
	} else {
		b := rep.Battery
		wear := ""
		if b.HasWear {
			wear = strconv.Itoa(b.WearPct) + "%"
		}
		r.table([]string{"NAME", "CHARGE", "WEAR", "CYCLES", "TEMP", "ST", "NOTE"}, [][]string{{
			b.Full, fmt.Sprintf("%d%% (%s)", b.Charge, b.State), wear,
			strconv.Itoa(b.Cycles), r.tempCell(b.Temp), r.stateCell(b.Status), b.Reason,
		}})
	}

	// Fans
	r.section("Fans")
	if len(rep.Fans.Fans) == 0 {
		r.dimln("(no fan tachometer exposed)")
	} else {
		var rows [][]string
		for _, f := range rep.Fans.Fans {
			max := ""
			if f.Max > 0 {
				max = strconv.Itoa(f.Max) + " RPM"
			}
			rows = append(rows, []string{f.Label, strconv.Itoa(f.RPM) + " RPM", max})
		}
		r.table([]string{"FAN", "RPM", "MAX"}, rows)
	}
	r.b.WriteString(fmt.Sprintf("  → %s  %s%s%s\n", r.stateCell(rep.Fans.Status), r.c.dim, rep.Fans.Reason, r.c.rst))

	// NICs
	r.section("Network cards")
	if len(rep.NICs) == 0 {
		r.dimln("(no network interface found)")
	} else {
		var rows [][]string
		for _, n := range rep.NICs {
			addr := orValue(n.Addrs, "—")
			if n.Speed != "" {
				addr += "  @" + n.Speed
			}
			rows = append(rows, []string{
				n.Iface, n.Kind, trunc(n.Name, 30), n.Link,
				r.tempCell(n.Temp), r.stateCell(n.Status), addr,
			})
		}
		r.table([]string{"IFACE", "TYPE", "NAME", "LINK", "TEMP", "ST", "ADDRESSES"}, rows)
	}

	// Summary
	r.section("Summary")
	r.summary(rep)
	r.b.WriteByte('\n')
	r.legend()
}

// Synthetic renders the one-line-per-component view.
func (r *R) Synthetic(rep sensors.Report) {
	r.b.WriteString(fmt.Sprintf("%s%sHardware health — synthetic view%s  (%s)\n",
		r.c.bold, r.c.blu, r.c.rst, rep.Time.Format("15:04:05")))
	r.legend()
	r.summary(rep)
}

// line is one row of the synthetic summary.
type line struct {
	status sensors.Status
	label  string
	reason string
	temp   *sensors.Temp
}

func (r *R) summaryLines(rep sensors.Report) []line {
	var ls []line
	ls = append(ls, line{rep.CPU.Status, "CPU", rep.CPU.Reason, rep.CPU.Temp})
	ls = append(ls, line{rep.RAM.Status, "RAM", rep.RAM.Reason, rep.RAM.Temp})
	for i, g := range rep.GPUs {
		ls = append(ls, line{g.Status, fmt.Sprintf("GPU #%d: %s", i+1, g.Name), g.Reason, g.Temp})
	}
	for i, d := range rep.Disks {
		reason := d.Reason
		if d.Temp != nil {
			reason += fmt.Sprintf(" (%d°C)", int(d.Temp.C+0.5))
		}
		ls = append(ls, line{d.Status, fmt.Sprintf("Disk #%d: %s", i+1, d.Model), reason, d.Temp})
	}
	if rep.Battery != nil {
		ls = append(ls, line{rep.Battery.Status, "Battery: " + rep.Battery.Name, rep.Battery.Reason, rep.Battery.Temp})
	}
	ls = append(ls, line{rep.Fans.Status, "Fans", rep.Fans.Reason, nil})
	for _, n := range rep.NICs {
		ls = append(ls, line{n.Status, "NIC " + n.Iface, n.Reason, n.Temp})
	}
	return ls
}

func (r *R) summary(rep sensors.Report) {
	for _, l := range r.summaryLines(rep) {
		label := trunc(l.label, 30)
		// Fixed-width temp cell (5 visible chars) keeps the reason column aligned.
		var tcell string
		if l.temp != nil {
			v := fmt.Sprintf("%3d", int(l.temp.C+0.5))
			if r.o.Color {
				tcell = r.gradTemp(l.temp) + v + "°C" + r.c.rst
			} else {
				tcell = v + "°C"
			}
		} else {
			tcell = "     "
		}
		var col string
		switch l.status {
		case sensors.OK:
			col = r.c.grn
		case sensors.KO:
			col = r.c.red
		case sensors.WRN:
			col = r.c.yel
		default:
			col = r.c.dim
		}
		r.b.WriteString(fmt.Sprintf("  [%s%-3s%s] %-30s %s   %s%s%s\n",
			col, l.status.String(), r.c.rst, label, tcell, r.c.dim, l.reason, r.c.rst))
	}
}

func (r *R) legend() {
	r.b.WriteString(fmt.Sprintf("  %stemp scale%s  %s  %scool → hot (each temp colored on its own min→max range)%s\n",
		r.c.dim, r.c.rst, r.gradBar(24), r.c.dim, r.c.rst))
}

func orUnknown(s string) string { return orValue(s, "unknown") }
func orValue(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
