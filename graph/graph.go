// Package graph renders the long-term CSV time-series into a single
// self-contained interactive HTML dashboard (Plotly), one stacked panel per
// dimension, with dashed warn/critical threshold overlays and synchronized
// time-range controls. Mirrors nstat's graph approach.
package graph

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"sort"
	"time"
)

// Point is one downsampled sample.
type Point struct {
	T time.Time
	V float64
}

// Panel is one dimension's data + display metadata.
type Panel struct {
	Key   string
	Title string
	Unit  string
	Warn  float64
	Crit  float64
	Data  []Point
}

// Downsample reduces a series to at most maxPts points using LTTB
// (Largest-Triangle-Three-Buckets), preserving visual peaks/troughs.
func Downsample(pts []Point, maxPts int) []Point {
	n := len(pts)
	if maxPts < 3 || n <= maxPts {
		return pts
	}
	sampled := make([]Point, 0, maxPts)
	sampled = append(sampled, pts[0]) // always keep the first

	bucketSize := float64(n-2) / float64(maxPts-2)
	a := 0 // index of the last selected point
	for i := 0; i < maxPts-2; i++ {
		// Range for the next bucket (average point).
		avgStart := int(float64(i+1)*bucketSize) + 1
		avgEnd := int(float64(i+2)*bucketSize) + 1
		if avgEnd > n {
			avgEnd = n
		}
		var avgT, avgV float64
		for j := avgStart; j < avgEnd; j++ {
			avgT += float64(pts[j].T.UnixNano())
			avgV += pts[j].V
		}
		cnt := float64(avgEnd - avgStart)
		if cnt == 0 {
			cnt = 1
		}
		avgT /= cnt
		avgV /= cnt

		// Range for this bucket.
		rangeStart := int(float64(i)*bucketSize) + 1
		rangeEnd := int(float64(i+1)*bucketSize) + 1
		ax := float64(pts[a].T.UnixNano())
		ay := pts[a].V

		maxArea := -1.0
		next := rangeStart
		for j := rangeStart; j < rangeEnd && j < n; j++ {
			area := (ax-avgT)*(pts[j].V-ay) - (ax-float64(pts[j].T.UnixNano()))*(avgV-ay)
			if area < 0 {
				area = -area
			}
			if area > maxArea {
				maxArea, next = area, j
			}
		}
		sampled = append(sampled, pts[next])
		a = next
	}
	sampled = append(sampled, pts[n-1]) // always keep the last
	return sampled
}

// jsPanel is the JSON shape handed to the page's JavaScript.
//
// Y holds pointers so a gap in the data (e.g. the laptop was off) can be
// emitted as JSON null, which Plotly renders as a break in the line rather
// than interpolating a fake value across the missing period.
type jsPanel struct {
	Title string     `json:"title"`
	Unit  string     `json:"unit"`
	Warn  float64    `json:"warn"`
	Crit  float64    `json:"crit"`
	X     []string   `json:"x"`
	Y     []*float64 `json:"y"`
}

// gapThreshold returns the time delta above which two consecutive samples are
// considered to span a data gap (daemon was not running) rather than a normal
// sampling interval. It is derived from the data's own median spacing so it
// adapts to the configured --interval and to downsampling, then scaled up so
// ordinary jitter never trips it. Returns 0 when there are too few points to
// tell, which disables gap detection.
func gapThreshold(pts []Point) time.Duration {
	if len(pts) < 3 {
		return 0
	}
	deltas := make([]time.Duration, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		if d := pts[i].T.Sub(pts[i-1].T); d > 0 {
			deltas = append(deltas, d)
		}
	}
	if len(deltas) == 0 {
		return 0
	}
	sort.Slice(deltas, func(i, j int) bool { return deltas[i] < deltas[j] })
	median := deltas[len(deltas)/2]
	// Break the line at gaps larger than ~4 typical intervals (a couple of
	// missed samples is still a continuous line; a long absence is a gap).
	return 4 * median
}

// Generate writes the dashboard HTML to path.
func Generate(path string, panels []Panel, host string, since time.Time) error {
	var js []jsPanel
	for _, p := range panels {
		if len(p.Data) == 0 {
			continue
		}
		jp := jsPanel{Title: p.Title, Unit: p.Unit, Warn: p.Warn, Crit: p.Crit}
		gap := gapThreshold(p.Data)
		var last time.Time
		for i, pt := range p.Data {
			// Insert a null break when the daemon stopped sampling for a while
			// (e.g. the laptop was off), so Plotly leaves a gap instead of
			// drawing an interpolated line across the missing period.
			if i > 0 && gap > 0 && pt.T.Sub(last) > gap {
				jp.X = append(jp.X, last.Add(pt.T.Sub(last)/2).Format("2006-01-02 15:04:05"))
				jp.Y = append(jp.Y, nil)
			}
			v := round2(pt.V)
			jp.X = append(jp.X, pt.T.Format("2006-01-02 15:04:05"))
			jp.Y = append(jp.Y, &v)
			last = pt.T
		}
		js = append(js, jp)
	}
	data, err := json.Marshal(js)
	if err != nil {
		return err
	}

	rangeLabel := "all history"
	if !since.IsZero() {
		rangeLabel = fmt.Sprintf("since %s", since.Format("2006-01-02 15:04"))
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, map[string]any{
		"Host":      host,
		"Range":     rangeLabel,
		"Generated": time.Now().Format("2006-01-02 15:04:05"),
		"PanelJSON": template.JS(data),
	})
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

var tmpl = template.Must(template.New("dash").Parse(dashHTML))

const dashHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>hwstat — {{.Host}}</title>
<script src="https://cdn.plot.ly/plotly-2.32.0.min.js" charset="utf-8"></script>
<style>
  :root {
    --bg:#1e1e2e; --surface:#181825; --text:#cdd6f4; --subtext:#a6adc8;
    --blue:#89b4fa; --green:#a6e3a1; --yellow:#f9e2af; --red:#f38ba8; --grid:#313244;
  }
  * { box-sizing: border-box; }
  body { margin:0; background:var(--bg); color:var(--text);
         font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; }
  header { padding:16px 24px; border-bottom:1px solid var(--grid); }
  h1 { margin:0; font-size:18px; color:var(--blue); }
  .meta { color:var(--subtext); font-size:13px; margin-top:4px; }
  .controls { padding:12px 24px; display:flex; gap:8px; flex-wrap:wrap; align-items:center; }
  .controls span { color:var(--subtext); font-size:13px; margin-right:4px; }
  button { background:var(--surface); color:var(--text); border:1px solid var(--grid);
           border-radius:6px; padding:6px 12px; cursor:pointer; font-family:inherit; font-size:13px; }
  button:hover { border-color:var(--blue); color:var(--blue); }
  .panel { padding:0 12px; }
  .empty { padding:48px 24px; color:var(--subtext); }
</style>
</head>
<body>
<header>
  <h1>hwstat — {{.Host}}</h1>
  <div class="meta">long-term hardware health · {{.Range}} · generated {{.Generated}}</div>
</header>
<div class="controls">
  <span>range:</span>
  <button onclick="setRange(1)">1h</button>
  <button onclick="setRange(6)">6h</button>
  <button onclick="setRange(24)">24h</button>
  <button onclick="setRange(168)">7d</button>
  <button onclick="setRange(0)">all</button>
</div>
<div id="panels"></div>

<script>
const PANELS = {{.PanelJSON}};
const C = { bg:'#1e1e2e', surface:'#181825', text:'#cdd6f4', sub:'#a6adc8',
            blue:'#89b4fa', green:'#a6e3a1', yellow:'#f9e2af', red:'#f38ba8', grid:'#313244' };

const container = document.getElementById('panels');
const divs = [];

if (!PANELS.length) {
  container.innerHTML = '<div class="empty">No data yet. Let the daemon run, then re-run <code>hwstat graph</code>.</div>';
}

PANELS.forEach((p, i) => {
  const div = document.createElement('div');
  div.className = 'panel';
  div.id = 'panel' + i;
  container.appendChild(div);
  divs.push(div);

  const traces = [{
    x: p.x, y: p.y, mode: 'lines', name: p.title,
    line: { color: C.blue, width: 1.5 },
    connectgaps: false,
    fill: 'tozeroy', fillcolor: 'rgba(137,180,250,0.08)',
    hovertemplate: '%{x}<br>%{y} ' + p.unit + '<extra></extra>'
  }];

  const shapes = [];
  const ann = [];
  if (p.warn > 0) {
    shapes.push(thresh(p.warn, C.yellow));
    ann.push(threshLabel(p.warn, 'warn ' + p.warn + p.unit, C.yellow));
  }
  if (p.crit > 0) {
    shapes.push(thresh(p.crit, C.red));
    ann.push(threshLabel(p.crit, 'crit ' + p.crit + p.unit, C.red));
  }

  Plotly.newPlot(div, traces, {
    title: { text: p.title + (p.unit ? ' (' + p.unit + ')' : ''), font: { color: C.text, size: 14 }, x: 0.01 },
    paper_bgcolor: C.bg, plot_bgcolor: C.surface,
    font: { color: C.sub, size: 11 },
    margin: { l: 56, r: 24, t: 36, b: 28 },
    height: 220,
    xaxis: { gridcolor: C.grid, type: 'date' },
    yaxis: { gridcolor: C.grid },
    shapes: shapes, annotations: ann,
    showlegend: false
  }, { responsive: true, displayModeBar: false });

  // Sync zoom/pan across panels.
  div.on('plotly_relayout', (ev) => {
    if (syncing) return;
    if (ev['xaxis.range[0]'] === undefined && ev['xaxis.autorange'] === undefined) return;
    syncing = true;
    const update = ev['xaxis.autorange']
      ? { 'xaxis.autorange': true }
      : { 'xaxis.range': [ev['xaxis.range[0]'], ev['xaxis.range[1]']] };
    divs.forEach(d => { if (d !== div) Plotly.relayout(d, update); });
    syncing = false;
  });
});

let syncing = false;

function thresh(y, color) {
  return { type: 'line', xref: 'paper', x0: 0, x1: 1, y0: y, y1: y,
           line: { color: color, width: 1, dash: 'dash' } };
}
function threshLabel(y, text, color) {
  return { xref: 'paper', x: 1, y: y, xanchor: 'right', yanchor: 'bottom',
           text: text, showarrow: false, font: { color: color, size: 10 } };
}

function setRange(hours) {
  let update;
  if (hours === 0) {
    update = { 'xaxis.autorange': true };
  } else {
    const end = new Date();
    const start = new Date(end.getTime() - hours * 3600 * 1000);
    update = { 'xaxis.range': [fmt(start), fmt(end)] };
  }
  syncing = true;
  divs.forEach(d => Plotly.relayout(d, update));
  syncing = false;
}
function fmt(d) {
  const p = n => String(n).padStart(2, '0');
  return d.getFullYear() + '-' + p(d.getMonth()+1) + '-' + p(d.getDate()) + ' ' +
         p(d.getHours()) + ':' + p(d.getMinutes()) + ':' + p(d.getSeconds());
}
</script>
</body>
</html>
`
