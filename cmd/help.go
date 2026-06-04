package cmd

import "fmt"

const helpText = `hwstat — laptop hardware health monitor (daemon + long-term metrics)

USAGE
  hwstat <command> [options]

COMMANDS
  start [--interval N]   Start the background daemon (sample every N seconds, default 30).
  stop                   Gracefully stop the daemon.
  status [-s] [--no-color]
                         Print the realtime hardware report (fresh sample).
                         -s shows the synthetic one-line-per-component view.
  log                    Show the last 40 daemon log lines.
  graph [--hours N] [--no-open]
                         Build an interactive HTML dashboard from the long-term
                         CSV time-series (default: all history) and open it.
  -v, --version          Print version.
  -h, --help             Show this help.

DATA
  Samples are stored as one CSV time-series per dimension (csv_<dim>.csv) plus a
  state snapshot, pid and log file, under the per-user data directory
  (~/.local/share/hwstat on Linux). Logs and CSVs rotate every 24h (.1/.2/.3).

  Realtime temperatures are read straight from the kernel's sysfs (hwmon /
  thermal), so no lm_sensors or nvidia-smi is required. Run with sudo for SMART
  disk health and DIMM details.

EXIT CODE (status)
  0 all OK   1 at least one WRN   2 at least one KO
`

// Help prints usage.
func Help() { fmt.Print(helpText) }
