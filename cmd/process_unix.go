//go:build !windows

package cmd

import "syscall"

// processAlive reports whether a process with the given PID exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Signal 0 performs error checking without actually sending a signal.
	return syscall.Kill(pid, 0) == nil
}

// terminate sends SIGTERM to the process.
func terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
