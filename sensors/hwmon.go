package sensors

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// readStr reads a sysfs file and trims it. Returns "" on any error.
func readStr(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// readInt reads a sysfs integer file. ok=false on any error.
func readInt(path string) (int64, bool) {
	s := readStr(path)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// hwmonDirs returns every /sys/class/hwmon/hwmonN directory whose `name` file
// matches one of the wanted names (case-insensitive exact match).
func hwmonDirs(names ...string) []string {
	var out []string
	entries, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	for _, h := range entries {
		got := strings.ToLower(readStr(filepath.Join(h, "name")))
		if got == "" {
			continue
		}
		for _, want := range names {
			if got == strings.ToLower(want) {
				out = append(out, h)
				break
			}
		}
	}
	return out
}

// hwmonDir returns the first hwmon dir matching any of the names, or "".
func hwmonDir(names ...string) string {
	if d := hwmonDirs(names...); len(d) > 0 {
		return d[0]
	}
	return ""
}

// hwmonMaxTemp returns the maximum tempN_input (converted from milli-°C to °C)
// found in dir. ok=false if the directory exposes no temperature.
func hwmonMaxTemp(dir string) (float64, bool) {
	if dir == "" {
		return 0, false
	}
	inputs, _ := filepath.Glob(filepath.Join(dir, "temp*_input"))
	var max float64
	found := false
	for _, f := range inputs {
		v, ok := readInt(f)
		if !ok {
			continue
		}
		c := float64(v) / 1000.0
		if !found || c > max {
			max, found = c, true
		}
	}
	return max, found
}

// hwmonMaxTempByName combines the two helpers above over all matching dirs.
func hwmonMaxTempByName(names ...string) (float64, bool) {
	var max float64
	found := false
	for _, d := range hwmonDirs(names...) {
		if c, ok := hwmonMaxTemp(d); ok && (!found || c > max) {
			max, found = c, true
		}
	}
	return max, found
}

// hwmonMaxTempByPrefix is like hwmonMaxTempByName but matches any hwmon whose
// name starts with one of the prefixes (e.g. "iwlwifi" matches "iwlwifi_1").
func hwmonMaxTempByPrefix(prefixes ...string) (float64, bool) {
	var max float64
	found := false
	entries, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	for _, h := range entries {
		name := strings.ToLower(readStr(filepath.Join(h, "name")))
		for _, p := range prefixes {
			if strings.HasPrefix(name, strings.ToLower(p)) {
				if c, ok := hwmonMaxTemp(h); ok && (!found || c > max) {
					max, found = c, true
				}
				break
			}
		}
	}
	return max, found
}

// thermalZoneTemp returns the temperature (°C) of the first thermal_zone whose
// type matches any of the substrings, case-insensitive.
func thermalZoneTemp(typeSubstrs ...string) (float64, bool) {
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	for _, z := range zones {
		zt := strings.ToLower(readStr(filepath.Join(z, "type")))
		for _, sub := range typeSubstrs {
			if strings.Contains(zt, strings.ToLower(sub)) {
				if v, ok := readInt(filepath.Join(z, "temp")); ok {
					return float64(v) / 1000.0, true
				}
			}
		}
	}
	return 0, false
}
