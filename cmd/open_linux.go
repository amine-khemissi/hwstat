//go:build linux

package cmd

import "os/exec"

// openInBrowser opens a file/URL with the desktop default handler.
func openInBrowser(path string) error {
	return exec.Command("xdg-open", path).Start()
}
