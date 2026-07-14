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
