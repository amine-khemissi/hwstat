package cmd

import (
	"bufio"
	"fmt"
	"os"

	"github.com/amine-khemissi/hwstat/config"
)

// Log prints the last 40 lines of the daemon log.
func Log() {
	cfg := config.Default()
	f, err := os.Open(cfg.LogFile)
	if err != nil {
		fatal("no log yet (" + cfg.LogFile + ") — start the daemon with `hwstat start`")
	}
	defer f.Close()

	const keep = 40
	ring := make([]string, 0, keep)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if len(ring) == keep {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	for _, l := range ring {
		fmt.Println(l)
	}
}
