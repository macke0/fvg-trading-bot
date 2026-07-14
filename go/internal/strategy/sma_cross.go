package strategy

import (
	"fvgbot/internal/indicators"
	"fvgbot/internal/types"
)

// SMACross trades when price crosses its simple moving average, but only when
// the crossing bar carries above-average volume. The stop is placed beyond the
// most recent swing, and the target is a multiple of that risk.
type SMACross struct {
	SMAPeriod        int     // moving-average length / trend reference
	VolumePeriod     int     // window for the average-volume filter
	VolumeThreshold  float64 // required volume as a multiple of the average; 0 disables the filter
	SwingLookback    int     // bars to look back for the protective swing
	RiskReward       float64 // target distance as a multiple of the stop distance
	Buffer           float64 // padding beyond the swing for the stop
	EarlySessionOnly bool    // restrict entries to 09:30-12:00 ET
}

// NewSMACross returns an SMACross strategy with sensible defaults.
func NewSMACross() *SMACross {
	return &SMACross{
		SMAPeriod:        100,
		VolumePeriod:     20,
		VolumeThreshold:  1.2,
		SwingLookback:    10,
		RiskReward:       2.0,
		Buffer:           0.01,
		EarlySessionOnly: true,
	}
}

func (s *SMACross) Name() string { return "SMA_Cross_Volume" }

func (s *SMACross) OnBar(bar types.Bar, history []types.Bar) types.Signal {
	if len(history) < s.SMAPeriod+1 {
		return types.Signal{Action: types.Hold, Reason: "insufficient data"}
	}
	if s.EarlySessionOnly && !inEarlySession(bar.Timestamp) {
		return types.Signal{Action: types.Hold, Reason: "outside early session"}
	}

	current := history[len(history)-1]
	prev := history[len(history)-2]

	// SMA on the current bar vs. the bar before, to detect a crossing.
	currentSMA := indicators.SMA(history, s.SMAPeriod)
	prevSMA := indicators.SMA(history[:len(history)-1], s.SMAPeriod)

	// Require the crossing bar to trade on above-average volume.
	avgVol := indicators.AvgVolume(history[:len(history)-1], s.VolumePeriod)
	if float64(current.Volume) < avgVol*s.VolumeThreshold {
		return types.Signal{Action: types.Hold, Reason: "volume too low"}
	}

	// Cross up through the SMA -> go long.
	if prev.Close < prevSMA && current.Close > currentSMA {
		stop := indicators.SwingLow(history, s.SwingLookback) - s.Buffer
		risk := current.Close - stop
		return types.Signal{
			Action:   types.Buy,
			Size:     1.0,
			StopLoss: stop,
			Target:   current.Close + risk*s.RiskReward,
			Reason:   "SMA cross up with volume confirmation",
		}
	}

	// Cross down through the SMA -> go short.
	if prev.Close > prevSMA && current.Close < currentSMA {
		stop := indicators.SwingHigh(history, s.SwingLookback) + s.Buffer
		risk := stop - current.Close
		return types.Signal{
			Action:   types.Sell,
			Size:     1.0,
			StopLoss: stop,
			Target:   current.Close - risk*s.RiskReward,
			Reason:   "SMA cross down with volume confirmation",
		}
	}

	return types.Signal{Action: types.Hold, Reason: "no cross detected"}
}
