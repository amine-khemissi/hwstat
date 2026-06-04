package cmd

import (
	"fmt"
	"time"

	"github.com/amine-khemissi/hwstat/config"
)

// Stop gracefully terminates the daemon.
func Stop() {
	cfg := config.Default()
	pid := daemonRunning(cfg)
	if pid == 0 {
		fmt.Println("hwstat daemon is not running")
		return
	}
	if err := terminate(pid); err != nil {
		fatal("failed to signal pid " + fmt.Sprint(pid) + ": " + err.Error())
	}
	// Wait briefly for it to exit and clean up its pid file.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			fmt.Printf("hwstat daemon stopped (pid %d)\n", pid)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("hwstat daemon (pid %d) did not exit in time\n", pid)
}
