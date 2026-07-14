// Package strategy defines the Strategy interface and the concrete trading
// strategies. A strategy is stateless with respect to the backtest: it only
// reads the bar history it is given and returns a Signal.
package strategy

import (
	"time"

	"fvgbot/internal/types"
)

// Strategy inspects each new bar (with the history up to and including it) and
// returns a Signal telling the engine what to do.
type Strategy interface {
	// Name identifies the strategy in results output.
	Name() string
	// OnBar is called once per bar. history[len(history)-1] == bar.
	OnBar(bar types.Bar, history []types.Bar) types.Signal
}

var nyZone *time.Location

func init() {
	// America/New_York governs US equity market hours. If the tz database is
	// unavailable we fall back to UTC and inSession below effectively no-ops.
	nyZone, _ = time.LoadLocation("America/New_York")
}

// inEarlySession reports whether t (in New York time) falls in the first hours
// after the open, 09:30–12:00 ET, where these setups are most reliable.
func inEarlySession(unix int64) bool {
	t := time.Unix(unix, 0)
	if nyZone != nil {
		t = t.In(nyZone)
	}
	minutes := t.Hour()*60 + t.Minute()
	const open = 9*60 + 30 // 09:30
	const noon = 12 * 60    // 12:00
	return minutes >= open && minutes <= noon
}
