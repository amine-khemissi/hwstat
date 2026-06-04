// Package config holds hwstat's runtime configuration: the data directory,
// the paths of the files the daemon owns, and the sampling defaults.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Config is the resolved configuration for one hwstat run.
type Config struct {
	Dir string // data directory (per-platform, see dataDir)

	// Files the daemon owns, all inside Dir.
	LogFile   string
	StateFile string
	PIDFile   string
	GraphFile string

	// Timing.
	Interval       time.Duration // time between hardware samples
	LogRotateEvery time.Duration // rotate log + CSVs this often
}

// Default returns a Config with sensible defaults and all paths resolved
// inside the per-platform data directory.
func Default() Config {
	dir := dataDir()
	return Config{
		Dir:            dir,
		LogFile:        filepath.Join(dir, "hwstat.log"),
		StateFile:      filepath.Join(dir, "hwstat.state.json"),
		PIDFile:        filepath.Join(dir, "hwstat.pid"),
		GraphFile:      filepath.Join(dir, "hwstat_graph.html"),
		Interval:       30 * time.Second,
		LogRotateEvery: 24 * time.Hour,
	}
}

// EnsureDir creates the data directory if it does not exist.
func (c Config) EnsureDir() error { return os.MkdirAll(c.Dir, 0755) }

// dataDir resolves the platform-specific data directory, XDG-compliant on
// Linux.
func dataDir() string {
	const app = "hwstat"
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, app)
		}
		return filepath.Join(home(), "AppData", "Roaming", app)
	case "darwin":
		return filepath.Join(home(), "Library", "Application Support", app)
	default: // linux & other unix
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, app)
		}
		return filepath.Join(home(), ".local", "share", app)
	}
}

func home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return "."
}
