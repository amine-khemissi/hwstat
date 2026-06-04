// Command hwstat is a laptop hardware health monitor: a background daemon that
// samples CPU/GPU/RAM/disk/battery/fan/NIC temperatures and status from the
// kernel's sysfs, keeps long-term CSV time-series, and renders both a colored
// realtime report and an interactive HTML dashboard. Sibling to nstat.
package main

import (
	"fmt"
	"os"

	"github.com/amine-khemissi/hwstat/cmd"
	"github.com/amine-khemissi/hwstat/version"
)

const usage = "usage: hwstat {start [--interval N]|stop|status [-s]|log|graph [--hours N]|-h|-v}"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		cmd.Start(os.Args[2:])
	case "stop":
		cmd.Stop()
	case "status":
		cmd.Status(os.Args[2:])
	case "log":
		cmd.Log()
	case "graph":
		cmd.Graph(os.Args[2:])
	case "-h", "--help", "help":
		cmd.Help()
	case "-v", "--version", "version":
		fmt.Printf("hwstat %s\n", version.String())
	case "_daemon":
		cmd.Daemon(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "\x1b[91munknown command: %s\x1b[0m\n", os.Args[1])
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}
}
