// Package state persists the daemon's current snapshot to a JSON file so that
// `hwstat status` (and any other reader) can show what the daemon last saw,
// without taking its own samples.
package state

import (
	"encoding/json"
	"os"
	"time"
)

// State is the daemon's last snapshot.
type State struct {
	Host       string             `json:"host"`
	PID        int                `json:"pid"`
	StartedAt  time.Time          `json:"started_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
	IntervalS  int                `json:"interval_s"`
	Samples    int                `json:"samples"`
	Worst      string             `json:"worst"` // OK / WRN / KO
	Metrics    map[string]float64 `json:"metrics"`
	Components []Component        `json:"components"`
}

// Component is a per-component verdict mirrored from the realtime view.
type Component struct {
	Name   string   `json:"name"`
	Status string   `json:"status"` // OK / WRN / KO / NA
	Reason string   `json:"reason"`
	Temp   *float64 `json:"temp,omitempty"`
}

// Write atomically writes the state to path.
func Write(path string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Read loads the state from path.
func Read(path string) (State, error) {
	var s State
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}
