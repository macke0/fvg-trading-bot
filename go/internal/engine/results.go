package engine

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// WriteResults writes all trade records to a CSV at path (creating parent dirs).
// Columns: Strategy, Symbol, Timestamp, Action, Price, ProfitLoss.
func WriteResults(path string, records []TradeRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	w := csv.NewWriter(file)
	defer w.Flush()

	if err := w.Write([]string{"Strategy", "Symbol", "Timestamp", "Action", "Price", "ProfitLoss"}); err != nil {
		return err
	}
	for _, r := range records {
		row := []string{
			r.Strategy,
			r.Symbol,
			strconv.FormatInt(r.Timestamp, 10),
			r.Action,
			strconv.FormatFloat(r.Price, 'f', 4, 64),
			strconv.FormatFloat(r.ProfitLoss, 'f', 4, 64),
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// Summary aggregates closed-trade performance for one strategy.
type Summary struct {
	Strategy    string
	Trades      int
	Wins        int
	TotalPnL    float64
	MaxDrawdown float64
}

// WinRate is the fraction of closed trades that were profitable, as a percent.
func (s Summary) WinRate() float64 {
	if s.Trades == 0 {
		return 0
	}
	return float64(s.Wins) / float64(s.Trades) * 100
}

// Summarize computes per-strategy stats from the closed trades in records,
// ordered by first appearance.
func Summarize(records []TradeRecord) []Summary {
	type acc struct {
		sum      *Summary
		cumPnL   float64
		peakPnL  float64
	}
	byName := map[string]*acc{}
	var order []string

	// records are appended in time order per (strategy, symbol); for drawdown we
	// want a single equity curve per strategy across all symbols, ordered by
	// timestamp. Collect then sort.
	closed := make([]TradeRecord, 0, len(records))
	for _, r := range records {
		if strings.HasPrefix(r.Action, "CLOSE") {
			closed = append(closed, r)
		}
	}
	sort.SliceStable(closed, func(i, j int) bool {
		return closed[i].Timestamp < closed[j].Timestamp
	})

	for _, r := range closed {
		a, ok := byName[r.Strategy]
		if !ok {
			a = &acc{sum: &Summary{Strategy: r.Strategy}}
			byName[r.Strategy] = a
			order = append(order, r.Strategy)
		}
		a.sum.Trades++
		a.sum.TotalPnL += r.ProfitLoss
		if r.ProfitLoss > 0 {
			a.sum.Wins++
		}
		a.cumPnL += r.ProfitLoss
		if a.cumPnL > a.peakPnL {
			a.peakPnL = a.cumPnL
		}
		if dd := a.peakPnL - a.cumPnL; dd > a.sum.MaxDrawdown {
			a.sum.MaxDrawdown = dd
		}
	}

	out := make([]Summary, 0, len(order))
	for _, name := range order {
		out = append(out, *byName[name].sum)
	}
	return out
}

// PrintSummary writes a human-readable performance table to stdout.
func PrintSummary(records []TradeRecord) {
	line := strings.Repeat("=", 55)
	fmt.Println(line)
	fmt.Println("BACKTEST SUMMARY (closed trades)")
	fmt.Println(line)

	summaries := Summarize(records)
	if len(summaries) == 0 {
		fmt.Println("No closed trades.")
		return
	}
	for _, s := range summaries {
		fmt.Printf("\n--- %s ---\n", s.Strategy)
		fmt.Printf("Closed trades:     %d\n", s.Trades)
		fmt.Printf("Win rate:          %.2f%%\n", s.WinRate())
		fmt.Printf("Total profit/loss: %.2f\n", s.TotalPnL)
		fmt.Printf("Max drawdown:      %.2f\n", s.MaxDrawdown)
	}
}
