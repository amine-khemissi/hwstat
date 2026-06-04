package cmd

import (
	"fmt"
	"os"

	"github.com/amine-khemissi/hwstat/config"
	"github.com/amine-khemissi/hwstat/render"
	"github.com/amine-khemissi/hwstat/sensors"
	"github.com/amine-khemissi/hwstat/state"
)

// Status prints the realtime hardware report. It always takes a fresh sample
// (so it works even when the daemon is not running) and, when the daemon is up,
// notes how long it has been collecting and how many samples it holds.
func Status(args []string) {
	synthetic := false
	noColor := false
	for _, a := range args {
		switch a {
		case "-s", "--synthetic":
			synthetic = true
		case "--no-color":
			noColor = true
		default:
			fatal("unknown status option: " + a)
		}
	}

	rep := sensors.Collect()
	r := render.New(render.AutoOptions(noColor))
	if synthetic {
		r.Synthetic(rep)
	} else {
		r.Full(rep)
	}
	fmt.Print(r.String())

	// Footer: daemon collection status.
	cfg := config.Default()
	if pid := daemonRunning(cfg); pid != 0 {
		if s, err := state.Read(cfg.StateFile); err == nil {
			fmt.Printf("\n  daemon: collecting since %s — %d samples (pid %d)\n",
				s.StartedAt.Format("2006-01-02 15:04"), s.Samples, pid)
		} else {
			fmt.Printf("\n  daemon: running (pid %d)\n", pid)
		}
	} else if !synthetic {
		fmt.Fprintf(os.Stdout, "\n  daemon: not running — start long-term metrics with `hwstat start`\n")
	}

	os.Exit(exitCode(rep.Worst()))
}

func exitCode(s sensors.Status) int {
	switch s {
	case sensors.KO:
		return 2
	case sensors.WRN:
		return 1
	default:
		return 0
	}
}
