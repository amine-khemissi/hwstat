package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/amine-khemissi/hwstat/config"
)

// Start launches the background daemon.
func Start(args []string) {
	cfg := config.Default()

	// Flag parsing: --interval N (seconds).
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--interval":
			i++
			if i >= len(args) {
				fatal("--interval needs a value (seconds)")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				fatal("--interval must be a positive integer")
			}
			cfg.Interval = time.Duration(n) * time.Second
		default:
			fatal("unknown start option: " + args[i])
		}
	}

	if pid := daemonRunning(cfg); pid != 0 {
		fatal("daemon already running (pid " + strconv.Itoa(pid) + ") — use `hwstat stop` first")
	}
	if err := cfg.EnsureDir(); err != nil {
		fatal("cannot create data dir: " + err.Error())
	}

	exe, err := os.Executable()
	if err != nil {
		fatal("cannot find own executable: " + err.Error())
	}

	c := exec.Command(exe, "_daemon", strconv.Itoa(int(cfg.Interval.Seconds())))
	setSysProcAttr(c)
	c.Stdout = nil
	c.Stderr = nil
	if err := c.Start(); err != nil {
		fatal("failed to start daemon: " + err.Error())
	}
	c.Process.Release()

	// Wait up to 2s for the daemon to come up and write its pid.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pid := daemonRunning(cfg); pid != 0 {
			fmt.Printf("hwstat daemon started (pid %d)\n", pid)
			fmt.Printf("  interval : %s\n", cfg.Interval)
			fmt.Printf("  data dir : %s\n", cfg.Dir)
			fmt.Printf("  status   : hwstat status   |   graph: hwstat graph\n")
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	fatal("daemon did not come up within 2s — check " + cfg.LogFile)
}
