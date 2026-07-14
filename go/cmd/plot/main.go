// Command plot reads a backtest results CSV and writes an SVG equity curve
// (cumulative P/L over time) with one line per strategy. It has no external
// dependencies, so it runs anywhere the Go engine does.
//
// Usage:
//
//	go run ./cmd/plot --in ../output/results.csv --out ../output/equity.svg
package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// point is one closed-trade step in a strategy's equity curve.
type point struct {
	ts  int64
	cum float64
}

func main() {
	in := flag.String("in", "../output/results.csv", "results CSV to read")
	out := flag.String("out", "../output/equity.svg", "SVG file to write")
	flag.Parse()

	curves, err := loadCurves(*in)
	if err != nil {
		log.Fatal(err)
	}
	if len(curves) == 0 {
		log.Fatal("no closed trades found in results")
	}

	svg := renderSVG(curves, 960, 540)
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, []byte(svg), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s (%d strateg%s)", *out, len(curves), plural(len(curves)))
}

// loadCurves reads closed trades and builds a cumulative-P/L curve per strategy,
// ordered by timestamp.
func loadCurves(path string) (map[string][]point, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReader(f))
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("results file has no data rows")
	}

	col := map[string]int{}
	for i, name := range rows[0] {
		col[strings.TrimSpace(name)] = i
	}
	need := []string{"Strategy", "Timestamp", "Action", "ProfitLoss"}
	for _, c := range need {
		if _, ok := col[c]; !ok {
			return nil, fmt.Errorf("results missing column %q", c)
		}
	}

	type row struct {
		strat string
		ts    int64
		pnl   float64
	}
	var closed []row
	for _, rec := range rows[1:] {
		if !strings.HasPrefix(rec[col["Action"]], "CLOSE") {
			continue
		}
		ts, _ := strconv.ParseInt(rec[col["Timestamp"]], 10, 64)
		pnl, _ := strconv.ParseFloat(rec[col["ProfitLoss"]], 64)
		closed = append(closed, row{rec[col["Strategy"]], ts, pnl})
	}

	byStrat := map[string][]row{}
	for _, r := range closed {
		byStrat[r.strat] = append(byStrat[r.strat], r)
	}

	curves := map[string][]point{}
	for strat, rows := range byStrat {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].ts < rows[j].ts })
		var cum float64
		pts := make([]point, 0, len(rows))
		for _, r := range rows {
			cum += r.pnl
			pts = append(pts, point{ts: r.ts, cum: cum})
		}
		curves[strat] = pts
	}
	return curves, nil
}

// palette of line colors, reused cyclically.
var palette = []string{"#00E676", "#29B6F6", "#FFD700", "#AB47BC", "#FF7043"}

func renderSVG(curves map[string][]point, w, h int) string {
	const (
		mL, mR, mT, mB = 60, 180, 40, 50 // margins (right is wide for the legend)
	)
	plotW := w - mL - mR
	plotH := h - mT - mB

	// Global data ranges across all curves (P/L range always includes 0).
	var minTs, maxTs int64
	minPnl, maxPnl := 0.0, 0.0
	first := true
	for _, pts := range curves {
		for _, p := range pts {
			if first {
				minTs, maxTs = p.ts, p.ts
				first = false
			}
			if p.ts < minTs {
				minTs = p.ts
			}
			if p.ts > maxTs {
				maxTs = p.ts
			}
			if p.cum < minPnl {
				minPnl = p.cum
			}
			if p.cum > maxPnl {
				maxPnl = p.cum
			}
		}
	}
	if maxTs == minTs {
		maxTs = minTs + 1
	}
	if maxPnl == minPnl {
		maxPnl = minPnl + 1
	}

	// Data -> pixel mappers.
	x := func(ts int64) float64 {
		return float64(mL) + float64(ts-minTs)/float64(maxTs-minTs)*float64(plotW)
	}
	y := func(v float64) float64 {
		return float64(mT) + (maxPnl-v)/(maxPnl-minPnl)*float64(plotH)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="system-ui,Segoe UI,sans-serif">`, w, h, w, h)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#121212"/>`, w, h)
	fmt.Fprintf(&b, `<text x="%d" y="26" fill="#FFFFFF" font-size="18" font-weight="bold">Equity Curve — cumulative P/L per strategy</text>`, mL)

	// Zero line.
	zeroY := y(0)
	fmt.Fprintf(&b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#555" stroke-width="1" stroke-dasharray="4 4"/>`,
		mL, zeroY, mL+plotW, zeroY)
	fmt.Fprintf(&b, `<text x="%d" y="%.1f" fill="#888" font-size="11" text-anchor="end">0</text>`, mL-6, zeroY+4)

	// Axis frame.
	fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#404040"/>`, mL, mT, mL, mT+plotH)
	fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#404040"/>`, mL, mT+plotH, mL+plotW, mT+plotH)

	// Y range labels.
	fmt.Fprintf(&b, `<text x="%d" y="%.1f" fill="#888" font-size="11" text-anchor="end">%.0f</text>`, mL-6, y(maxPnl)+4, maxPnl)
	fmt.Fprintf(&b, `<text x="%d" y="%.1f" fill="#888" font-size="11" text-anchor="end">%.0f</text>`, mL-6, y(minPnl)+4, minPnl)

	// Deterministic strategy order for stable colors/legend.
	names := make([]string, 0, len(curves))
	for name := range curves {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, name := range names {
		pts := curves[name]
		color := palette[i%len(palette)]

		var path strings.Builder
		for j, p := range pts {
			cmd := "L"
			if j == 0 {
				cmd = "M"
			}
			fmt.Fprintf(&path, "%s%.1f %.1f ", cmd, x(p.ts), y(p.cum))
		}
		fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="%s" stroke-width="2"/>`, strings.TrimSpace(path.String()), color)

		// Legend entry with the final P/L.
		final := 0.0
		if len(pts) > 0 {
			final = pts[len(pts)-1].cum
		}
		ly := mT + 10 + i*22
		fmt.Fprintf(&b, `<rect x="%d" y="%d" width="14" height="14" fill="%s"/>`, mL+plotW+16, ly, color)
		fmt.Fprintf(&b, `<text x="%d" y="%d" fill="#E0E0E0" font-size="12">%s (%.1f)</text>`,
			mL+plotW+36, ly+12, name, final)
	}

	b.WriteString(`</svg>`)
	return b.String()
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
