// Package daemon runs the background sampling loop: every cfg.Interval it takes
// a full hardware snapshot, appends each numeric dimension to its CSV
// time-series, writes the current state JSON, logs a one-line summary, and
// rotates the log + CSVs every cfg.LogRotateEvery.
package daemon

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/amine-khemissi/hwstat/config"
	"github.com/amine-khemissi/hwstat/dim"
	"github.com/amine-khemissi/hwstat/sensors"
	"github.com/amine-khemissi/hwstat/state"
	"github.com/amine-khemissi/hwstat/store"
)

// Run is the daemon entry point. It blocks until SIGINT/SIGTERM.
func Run(cfg config.Config) {
	if err := cfg.EnsureDir(); err != nil {
		fmt.Fprintln(os.Stderr, "hwstat: cannot create data dir:", err)
		os.Exit(1)
	}

	logf, err := os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hwstat: cannot open log:", err)
		os.Exit(1)
	}
	lg := log.New(logf, "", log.LstdFlags)

	pid := os.Getpid()
	if err := os.WriteFile(cfg.PIDFile, []byte(fmt.Sprintf("%d\n", pid)), 0644); err != nil {
		lg.Println("cannot write pid file:", err)
		os.Exit(1)
	}

	host, _ := os.Hostname()
	started := time.Now()
	samples := 0
	prevWorst := ""   // last logged worst status
	prevProblem := "" // last logged problem text

	lg.Printf("daemon started (pid %d, interval %s, dir %s)", pid, cfg.Interval, cfg.Dir)

	// Graceful shutdown.
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigc
		lg.Printf("received %s — shutting down after %d samples", s, samples)
		os.Remove(cfg.PIDFile)
		logf.Close()
		os.Exit(0)
	}()

	sample := func() {
		rep := sensors.Collect()
		now := rep.Time
		metrics := rep.Metrics()

		for _, d := range dim.All {
			if v, ok := metrics[d.Key]; ok {
				path := filepath.Join(cfg.Dir, d.CSVFile())
				if err := store.Append(path, d.Key, now, v); err != nil {
					lg.Println("csv append error:", err)
				}
			}
		}

		snap := state.State{
			Host:      host,
			PID:       pid,
			StartedAt: started,
			UpdatedAt: now,
			IntervalS: int(cfg.Interval.Seconds()),
			Samples:   samples,
			Worst:     rep.Worst().String(),
			Metrics:   metrics,
		}
		for _, l := range componentLines(rep) {
			snap.Components = append(snap.Components, l)
		}
		if err := state.Write(cfg.StateFile, snap); err != nil {
			lg.Println("state write error:", err)
		}

		samples++

		// Log on state transitions (so a persistent "expected" WRN — idle dGPU,
		// unplugged cable — is recorded once, not every sample), plus a periodic
		// heartbeat so the log shows the daemon is alive and what it's seeing.
		worst := rep.Worst().String()
		problem := firstProblem(rep)
		switch {
		case worst != prevWorst || problem != prevProblem:
			if worst == "OK" {
				lg.Printf("sample %d: recovered — all OK", samples)
			} else {
				lg.Printf("sample %d: %s — %s", samples, worst, problem)
			}
			prevWorst, prevProblem = worst, problem
		case samples%120 == 1:
			// Heartbeat (~every hour at the 30s default).
			lg.Printf("sample %d: %s (cpu %s)", samples, worst, tempStr(rep.CPU.Temp))
		}
	}

	// First sample immediately, then on the interval.
	sample()
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	rotate := time.NewTicker(cfg.LogRotateEvery)
	defer rotate.Stop()

	for {
		select {
		case <-ticker.C:
			sample()
		case <-rotate.C:
			lg.Println("rotating log + CSV files")
			logf.Close()
			rotateFile(cfg.LogFile, 3)
			store.RotateCSVs(cfg.Dir)
			logf, err = os.OpenFile(cfg.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				fmt.Fprintln(os.Stderr, "hwstat: cannot reopen log:", err)
				os.Exit(1)
			}
			lg.SetOutput(logf)
		}
	}
}

func componentLines(rep sensors.Report) []state.Component {
	conv := func(name string, s sensors.Status, reason string, t *sensors.Temp) state.Component {
		c := state.Component{Name: name, Status: s.String(), Reason: reason}
		if t != nil {
			v := t.C
			c.Temp = &v
		}
		return c
	}
	var out []state.Component
	out = append(out, conv("CPU", rep.CPU.Status, rep.CPU.Reason, rep.CPU.Temp))
	out = append(out, conv("RAM", rep.RAM.Status, rep.RAM.Reason, rep.RAM.Temp))
	for i, g := range rep.GPUs {
		out = append(out, conv(fmt.Sprintf("GPU #%d: %s", i+1, g.Name), g.Status, g.Reason, g.Temp))
	}
	for i, d := range rep.Disks {
		out = append(out, conv(fmt.Sprintf("Disk #%d: %s", i+1, d.Model), d.Status, d.Reason, d.Temp))
	}
	if rep.Battery != nil {
		out = append(out, conv("Battery: "+rep.Battery.Name, rep.Battery.Status, rep.Battery.Reason, rep.Battery.Temp))
	}
	out = append(out, conv("Fans", rep.Fans.Status, rep.Fans.Reason, nil))
	for _, n := range rep.NICs {
		out = append(out, conv("NIC "+n.Iface, n.Status, n.Reason, n.Temp))
	}
	return out
}

func firstProblem(rep sensors.Report) string {
	for _, l := range componentLines(rep) {
		if l.Status == "WRN" || l.Status == "KO" {
			return l.Name + ": " + l.Reason
		}
	}
	return ""
}

func tempStr(t *sensors.Temp) string {
	if t == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.0f°C", t.C)
}

// rotateFile mirrors store's single-file rotation for the log file.
func rotateFile(path string, maxBackups int) {
	os.Remove(fmt.Sprintf("%s.%d", path, maxBackups))
	for i := maxBackups - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
	}
	if _, err := os.Stat(path); err == nil {
		os.Rename(path, path+".1")
	}
}
