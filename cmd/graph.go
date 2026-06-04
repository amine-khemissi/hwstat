package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/amine-khemissi/hwstat/config"
	"github.com/amine-khemissi/hwstat/dim"
	"github.com/amine-khemissi/hwstat/graph"
	"github.com/amine-khemissi/hwstat/store"
)

// Graph builds the HTML dashboard from the long-term CSV series and opens it.
func Graph(args []string) {
	cfg := config.Default()

	hours := 0 // 0 = all history
	open := true
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--hours":
			i++
			if i >= len(args) {
				fatal("--hours needs a value")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				fatal("--hours must be a non-negative integer")
			}
			hours = n
		case "--no-open":
			open = false
		default:
			fatal("unknown graph option: " + args[i])
		}
	}

	var since time.Time
	if hours > 0 {
		since = time.Now().Add(-time.Duration(hours) * time.Hour)
	}

	const maxPts = 3000
	var panels []graph.Panel
	total := 0
	for _, d := range dim.All {
		path := filepath.Join(cfg.Dir, d.CSVFile())
		pts, _ := store.Load(path, since)
		if len(pts) == 0 {
			continue
		}
		gp := make([]graph.Point, len(pts))
		for i, p := range pts {
			gp[i] = graph.Point{T: p.T, V: p.V}
		}
		gp = graph.Downsample(gp, maxPts)
		total += len(gp)
		panels = append(panels, graph.Panel{
			Key: d.Key, Title: d.Label, Unit: d.Unit,
			Warn: d.Warn, Crit: d.Crit, Data: gp,
		})
	}

	if len(panels) == 0 {
		fatal("no time-series data yet in " + cfg.Dir + " — start the daemon with `hwstat start` and let it run")
	}

	host, _ := os.Hostname()
	if err := graph.Generate(cfg.GraphFile, panels, host, since); err != nil {
		fatal("failed to write graph: " + err.Error())
	}
	fmt.Printf("wrote %s (%d panels, %d points)\n", cfg.GraphFile, len(panels), total)

	if open {
		if err := openInBrowser(cfg.GraphFile); err != nil {
			fmt.Printf("  open it manually: %s\n", cfg.GraphFile)
		}
	}
}
