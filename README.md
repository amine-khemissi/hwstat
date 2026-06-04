# hwstat — laptop hardware health monitor

A lightweight background **daemon** that continuously samples this machine's
hardware health and keeps **long-term metrics**, with a colored realtime report
and an interactive HTML dashboard.

Sibling to [`nstat`](https://github.com/amine-khemissi/nstat) (which does the
same for the network stack) — same command surface, same daemon model, same
data-dir / CSV time-series conventions. The realtime `status` view is a Go port
of this repo's `hwcheck.sh`.

## What it monitors

One numeric time-series is kept per dimension (only those the hardware exposes):

| Dimension        | Unit | Warn | Crit | Source                                  |
|------------------|------|------|------|-----------------------------------------|
| `cpu_temp`       | °C   | 85   | 95   | hwmon `coretemp`/`k10temp`, else thermal |
| `gpu_temp`       | °C   | 85   | 95   | hwmon `amdgpu`/`nouveau`, `nvidia-smi`  |
| `ram_used`       | %    | 90   | 97   | `/proc/meminfo`                         |
| `ram_temp`       | °C   | 80   | 90   | hwmon `spd5118`/`jc42`                   |
| `disk_temp`      | °C   | 70   | 85   | hwmon `nvme`/`drivetemp`                  |
| `wifi_temp`      | °C   | 75   | 90   | hwmon `iwlwifi*`/`mt76*`/`ath*`          |
| `battery_temp`   | °C   | 45   | 60   | `/sys/class/power_supply/BAT*/temp`     |
| `battery_charge` | %    | —    | —    | `…/capacity`                             |
| `fan_rpm`        | RPM  | —    | —    | hwmon `dell_smm`/`thinkpad`/any tach     |
| `load1`          | —    | —    | —    | `/proc/loadavg`                          |

Temperatures come straight from the kernel's **sysfs** (hwmon / thermal), so it
works **without** `lm_sensors` or `nvidia-smi`. Those, plus `smartctl` (SMART
disk health) and `dmidecode` (DIMM details), are used as a bonus when present.

## Build

Requires Go 1.26+.

```sh
git clone https://github.com/amine-khemissi/hwstat.git
cd hwstat && make build      # -> ./hwstat
make install                 # -> ~/.local/bin/hwstat
```

## Commands

```sh
hwstat start [--interval N]      # start daemon, sample every N seconds (default 30)
hwstat stop                      # gracefully stop the daemon
hwstat status                    # realtime report — one table per component family
hwstat status -s                 # synthetic — one OK/WRN/KO line per component
hwstat status --no-color         # plain text (logs / redirection)
hwstat log                       # last 40 daemon log lines
hwstat graph [--hours N]         # build + open the HTML dashboard (default: all history)
hwstat graph --hours 24 --no-open
hwstat -v                        # version
```

`status` exit code: `0` all OK, `1` at least one WRN, `2` at least one KO.

`status` always takes a fresh sample, so it works whether or not the daemon is
running; when the daemon is up it also reports how long it's been collecting.
Run `sudo hwstat status` to include SMART disk health and DIMM details.

## Data

Stored under the per-user data directory (XDG-compliant on Linux):

- Linux: `~/.local/share/hwstat/`
- macOS: `~/Library/Application Support/hwstat/`
- Windows: `%APPDATA%\hwstat\`

Files:

- `csv_<dim>.csv` — append-only time-series per dimension (`dimension,timestamp,value`)
- `hwstat.state.json` — last snapshot (read by `status`)
- `hwstat.pid` — daemon PID
- `hwstat.log` — event log
- `hwstat_graph.html` — last generated dashboard

The log and CSVs rotate every 24h (`.1` → `.2` → `.3`, then dropped). The graph
uses LTTB downsampling (max 3000 points/panel) and draws dashed warn/critical
threshold lines per panel, with synchronized pan/zoom and 1h/6h/24h/7d range
buttons.

## Run at boot (systemd user service)

```ini
# ~/.config/systemd/user/hwstat.service
[Unit]
Description=hwstat hardware health monitor

[Service]
ExecStart=%h/.local/bin/hwstat _daemon 30
Restart=on-failure

[Install]
WantedBy=default.target
```

```sh
systemctl --user enable --now hwstat.service
```
