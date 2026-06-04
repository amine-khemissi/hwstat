package cmd

import (
	"strconv"
	"time"

	"github.com/amine-khemissi/hwstat/config"
	"github.com/amine-khemissi/hwstat/daemon"
)

// Daemon is the internal `_daemon` entry point spawned by Start. It is not
// meant to be invoked directly.
func Daemon(args []string) {
	cfg := config.Default()
	if len(args) >= 1 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			cfg.Interval = time.Duration(n) * time.Second
		}
	}
	daemon.Run(cfg)
}
