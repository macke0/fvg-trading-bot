// Package data loads historical price bars from disk.
package data

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"fvgbot/internal/types"
)

// expected column order in the CSV header.
var columns = []string{"timestamp", "open", "high", "low", "close", "volume"}

// LoadCSV reads OHLCV bars from a CSV file with the header
// "timestamp,open,high,low,close,volume" (timestamp in unix seconds). Bars are
// returned sorted by timestamp ascending.
func LoadCSV(path string) ([]types.Bar, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	r := csv.NewReader(file)
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	idx, err := columnIndex(header)
	if err != nil {
		return nil, err
	}

	var bars []types.Bar
	line := 1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}

		bar, err := parseBar(rec, idx)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		bars = append(bars, bar)
	}

	sort.Slice(bars, func(i, j int) bool {
		return bars[i].Timestamp < bars[j].Timestamp
	})
	return bars, nil
}

// columnIndex maps each expected column name to its position in the header,
// tolerating extra/reordered columns.
func columnIndex(header []string) (map[string]int, error) {
	pos := map[string]int{}
	for i, name := range header {
		pos[strings.ToLower(strings.TrimSpace(name))] = i
	}
	idx := map[string]int{}
	for _, c := range columns {
		i, ok := pos[c]
		if !ok {
			return nil, fmt.Errorf("missing required column %q in header", c)
		}
		idx[c] = i
	}
	return idx, nil
}

func parseBar(rec []string, idx map[string]int) (types.Bar, error) {
	ts, err := strconv.ParseInt(strings.TrimSpace(rec[idx["timestamp"]]), 10, 64)
	if err != nil {
		return types.Bar{}, fmt.Errorf("timestamp: %w", err)
	}
	open, err := parseFloat(rec[idx["open"]])
	if err != nil {
		return types.Bar{}, fmt.Errorf("open: %w", err)
	}
	high, err := parseFloat(rec[idx["high"]])
	if err != nil {
		return types.Bar{}, fmt.Errorf("high: %w", err)
	}
	low, err := parseFloat(rec[idx["low"]])
	if err != nil {
		return types.Bar{}, fmt.Errorf("low: %w", err)
	}
	closePrice, err := parseFloat(rec[idx["close"]])
	if err != nil {
		return types.Bar{}, fmt.Errorf("close: %w", err)
	}
	// Volume may arrive as a float (e.g. "1234.0"); truncate to int.
	volF, err := parseFloat(rec[idx["volume"]])
	if err != nil {
		return types.Bar{}, fmt.Errorf("volume: %w", err)
	}

	return types.Bar{
		Timestamp: ts,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closePrice,
		Volume:    int64(volF),
	}, nil
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}
