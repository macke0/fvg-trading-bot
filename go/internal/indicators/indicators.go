// Package indicators provides small, pure technical-analysis helpers that
// strategies compose. Each operates on a slice of bars in chronological order.
package indicators

import "fvgbot/internal/types"

// SMA is the simple moving average of the closing price over the last `period`
// bars. Returns 0 if there is not enough history.
func SMA(bars []types.Bar, period int) float64 {
	if period <= 0 || len(bars) < period {
		return 0
	}
	sum := 0.0
	for i := len(bars) - period; i < len(bars); i++ {
		sum += bars[i].Close
	}
	return sum / float64(period)
}

// AvgVolume is the average volume over the last `period` bars.
func AvgVolume(bars []types.Bar, period int) float64 {
	if period <= 0 || len(bars) < period {
		return 0
	}
	var sum int64
	for i := len(bars) - period; i < len(bars); i++ {
		sum += bars[i].Volume
	}
	return float64(sum) / float64(period)
}

// SwingLow is the lowest low over the last `lookback` bars (clamped to the
// available history).
func SwingLow(bars []types.Bar, lookback int) float64 {
	if len(bars) == 0 {
		return 0
	}
	if lookback > len(bars) {
		lookback = len(bars)
	}
	lowest := bars[len(bars)-lookback].Low
	for i := len(bars) - lookback; i < len(bars); i++ {
		if bars[i].Low < lowest {
			lowest = bars[i].Low
		}
	}
	return lowest
}

// SwingHigh is the highest high over the last `lookback` bars (clamped to the
// available history).
func SwingHigh(bars []types.Bar, lookback int) float64 {
	if len(bars) == 0 {
		return 0
	}
	if lookback > len(bars) {
		lookback = len(bars)
	}
	highest := bars[len(bars)-lookback].High
	for i := len(bars) - lookback; i < len(bars); i++ {
		if bars[i].High > highest {
			highest = bars[i].High
		}
	}
	return highest
}

// aggregateGroup collapses a slice of bars into one bar: first open, last close,
// max high, min low, summed volume, timestamp of the first bar.
func aggregateGroup(group []types.Bar) types.Bar {
	agg := types.Bar{
		Timestamp: group[0].Timestamp,
		Open:      group[0].Open,
		High:      group[0].High,
		Low:       group[0].Low,
		Close:     group[len(group)-1].Close,
	}
	for _, b := range group {
		if b.High > agg.High {
			agg.High = b.High
		}
		if b.Low < agg.Low {
			agg.Low = b.Low
		}
		agg.Volume += b.Volume
	}
	return agg
}

// AggregateBars groups every `factor` consecutive bars into a single
// higher-timeframe bar (e.g. factor=3 turns 5-minute bars into 15-minute bars).
// A trailing partial group is dropped so every returned bar is complete.
func AggregateBars(bars []types.Bar, factor int) []types.Bar {
	if factor <= 1 {
		return bars
	}
	var out []types.Bar
	for i := 0; i+factor <= len(bars); i += factor {
		out = append(out, aggregateGroup(bars[i:i+factor]))
	}
	return out
}

// AggregateByDay is like AggregateBars but never groups bars from different
// calendar days together, so a higher-timeframe bar can't straddle the overnight
// gap. US regular-hours sessions fall entirely within one UTC day, so a UTC-day
// boundary cleanly separates trading days. Partial buckets at the end of a day
// are dropped, so every returned bar spans exactly `factor` input bars.
func AggregateByDay(bars []types.Bar, factor int) []types.Bar {
	if factor <= 1 {
		return bars
	}
	const secondsPerDay = 86400
	var out []types.Bar
	i := 0
	for i < len(bars) {
		day := bars[i].Timestamp / secondsPerDay
		j := i
		for j < len(bars) && j-i < factor && bars[j].Timestamp/secondsPerDay == day {
			j++
		}
		if j-i == factor {
			out = append(out, aggregateGroup(bars[i:j]))
		}
		i = j
	}
	return out
}
