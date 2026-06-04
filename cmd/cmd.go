// Package cmd implements the hwstat CLI subcommands.
package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/amine-khemissi/hwstat/config"
)

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "\x1b[91mhwstat: "+msg+"\x1b[0m")
	os.Exit(1)
}

// readPID reads and parses the daemon pid file.
func readPID(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, false
	}
	return pid, true
}

// daemonRunning returns the pid if the daemon is alive, else 0.
func daemonRunning(cfg config.Config) int {
	if pid, ok := readPID(cfg.PIDFile); ok && processAlive(pid) {
		return pid
	}
	return 0
}
