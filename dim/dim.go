// Package dim defines the numeric health dimensions that hwstat tracks over
// time. Each dimension maps to one CSV time-series file in the data dir and
// carries the warn/critical thresholds used both for severity coloring in the
// realtime view and for the dashed threshold overlays in the graph.
package dim

// Dim is the metadata for one numeric time-series.
type Dim struct {
	Key   string // stable identifier, also the CSV file stem: csv_<Key>.csv
	Label string // human label for the graph panel title
	Unit  string // "°C", "%", "RPM", ""
	Warn  float64
	Crit  float64
	GMin  float64 // gradient/axis lower bound
	GMax  float64 // gradient/axis upper bound

	// Most dimensions are "higher is worse" (temperatures, usage). A few are
	// informational (battery charge, fan RPM, load) and draw no threshold line
	// — those leave Warn/Crit at 0.
}

// All is the ordered set of dimensions. The order is the order panels appear in
// the graph. Only dimensions the hardware actually exposes get sampled.
var All = []Dim{
	{Key: "cpu_temp", Label: "CPU temperature", Unit: "°C", Warn: 85, Crit: 95, GMin: 35, GMax: 100},
	{Key: "gpu_temp", Label: "GPU temperature", Unit: "°C", Warn: 85, Crit: 95, GMin: 35, GMax: 100},
	{Key: "ram_used", Label: "RAM used", Unit: "%", Warn: 90, Crit: 97, GMin: 0, GMax: 100},
	{Key: "ram_temp", Label: "DIMM temperature", Unit: "°C", Warn: 80, Crit: 90, GMin: 30, GMax: 95},
	{Key: "disk_temp", Label: "Disk temperature", Unit: "°C", Warn: 70, Crit: 85, GMin: 25, GMax: 90},
	{Key: "wifi_temp", Label: "Wi-Fi temperature", Unit: "°C", Warn: 75, Crit: 90, GMin: 30, GMax: 95},
	{Key: "battery_temp", Label: "Battery temperature", Unit: "°C", Warn: 45, Crit: 60, GMin: 15, GMax: 65},
	{Key: "battery_charge", Label: "Battery charge", Unit: "%", GMin: 0, GMax: 100},
	{Key: "fan_rpm", Label: "Fan speed", Unit: "RPM", GMin: 0, GMax: 6000},
	{Key: "load1", Label: "Load average (1m)", Unit: "", GMin: 0, GMax: 8},
}

// CSVFile returns the per-dimension CSV file name (no directory).
func (d Dim) CSVFile() string { return "csv_" + d.Key + ".csv" }

// ByKey returns the Dim for a key, or zero value + false.
func ByKey(key string) (Dim, bool) {
	for _, d := range All {
		if d.Key == key {
			return d, true
		}
	}
	return Dim{}, false
}
