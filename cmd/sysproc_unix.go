//go:build !windows

package cmd

import (
	"os/exec"
	"syscall"
)

// setSysProcAttr detaches the spawned daemon into its own session so it
// survives the parent shell exiting.
func setSysProcAttr(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
