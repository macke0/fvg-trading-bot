package strategy

import (
	"fvgbot/internal/indicators"
	"fvgbot/internal/types"
)

// FVG (Fair Value Gap) trades imbalances left by a fast three-bar move, in the
// direction of the longer-term trend (price vs. SMA), taking entries only when
// price returns to "fill" the gap and shows it is holding.
type FVG struct {
	SMAPeriod  int     // trend filter length
	Lookback   int     // how many recent bars back to scan for an unfilled gap
	RiskReward float64 // target distance as a multiple of the stop distance
	Buffer     float64 // padding added beyond the gap edge for the stop
}

// NewFVG returns an FVG strategy with sensible defaults.
func NewFVG() *FVG {
	return &FVG{
		SMAPeriod:  100,
		Lookback:   10,
		RiskReward: 1.5,
		Buffer:     0.01,
	}
}

func (s *FVG) Name() string { return "FVG_Trend" }

// gap describes a detected fair value gap between Bottom and Top.
type gap struct {
	Top      float64
	Bottom   float64
	Bullish  bool
	Valid    bool
}

// detectGap looks at exactly three consecutive bars and reports a fair value
// gap if bar 1 and bar 3 do not overlap (bar 2 is the fast move between them).
func detectGap(three []types.Bar) gap {
	if len(three) < 3 {
		return gap{}
	}
	first, third := three[0], three[2]

	// Bullish gap: third bar's low sits entirely above first bar's high.
	if third.Low > first.High {
		return gap{Top: third.Low, Bottom: first.High, Bullish: true, Valid: true}
	}
	// Bearish gap: third bar's high sits entirely below first bar's low.
	if third.High < first.Low {
		return gap{Top: first.Low, Bottom: third.High, Bullish: false, Valid: true}
	}
	return gap{}
}

func (s *FVG) OnBar(bar types.Bar, history []types.Bar) types.Signal {
	if len(history) < s.SMAPeriod+3 {
		return types.Signal{Action: types.Hold, Reason: "insufficient data"}
	}
	if !inEarlySession(bar.Timestamp) {
		return types.Signal{Action: types.Hold, Reason: "outside early session"}
	}

	current := history[len(history)-1]
	sma := indicators.SMA(history, s.SMAPeriod)
	uptrend := current.Close > sma

	// Scan recent 3-bar windows for a gap, then check whether the current bar is
	// returning to test it. The window must end strictly before the current bar
	// (i+2 <= len-2), otherwise "the gap" would just be the current bar forming
	// it, not price retesting an earlier gap.
	for i := len(history) - s.Lookback; i <= len(history)-4; i++ {
		if i < 0 {
			continue
		}
		g := detectGap(history[i : i+3])
		if !g.Valid {
			continue
		}

		// The current bar must actually touch the gap.
		priceInGap := current.Low <= g.Top && current.High >= g.Bottom
		if !priceInGap {
			continue
		}

		isRed := current.Close < current.Open
		isGreen := current.Close > current.Open
		closeInGap := current.Close >= g.Bottom && current.Close <= g.Top
		openInGap := current.Open >= g.Bottom && current.Open <= g.Top

		if g.Bullish && uptrend {
			// Invalidation: a red bar that closes below the gap breaks it.
			if isRed && current.Close < g.Bottom {
				continue
			}
			redRetest := isRed && closeInGap    // red that held inside the gap
			greenBounce := isGreen && openInGap // green that bounced out of the gap
			if redRetest || greenBounce {
				stop := g.Bottom - s.Buffer
				risk := current.Close - stop
				return types.Signal{
					Action:   types.Buy,
					Size:     1.0,
					StopLoss: stop,
					Target:   current.Close + risk*s.RiskReward,
					Reason:   "bullish FVG hold/bounce in uptrend",
				}
			}
		}

		if !g.Bullish && !uptrend {
			// Invalidation: a green bar that closes above the gap breaks it.
			if isGreen && current.Close > g.Top {
				continue
			}
			greenRetest := isGreen && closeInGap // green that held inside the gap
			redRejection := isRed && openInGap   // red that rejected down out of the gap
			if greenRetest || redRejection {
				stop := g.Top + s.Buffer
				risk := stop - current.Close
				return types.Signal{
					Action:   types.Sell,
					Size:     1.0,
					StopLoss: stop,
					Target:   current.Close - risk*s.RiskReward,
					Reason:   "bearish FVG hold/rejection in downtrend",
				}
			}
		}
	}

	return types.Signal{Action: types.Hold, Reason: "no valid FVG setup"}
}
