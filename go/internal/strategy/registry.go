package strategy

import (
	"fmt"
	"sort"
	"strings"
)

// constructors maps a short CLI key to a factory for each available strategy.
var constructors = map[string]func() Strategy{
	"fvg": func() Strategy { return NewFVG() },
	"sma": func() Strategy { return NewSMACross() },
	"liq": func() Strategy { return NewLiquiditySweep() },
}

// Keys returns the available strategy keys, sorted.
func Keys() []string {
	keys := make([]string, 0, len(constructors))
	for k := range constructors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Select builds strategies for the given keys. The special key "all" (or an
// empty list) returns every available strategy, in sorted key order.
func Select(keys []string) ([]Strategy, error) {
	if len(keys) == 0 {
		keys = []string{"all"}
	}
	for _, k := range keys {
		if strings.EqualFold(k, "all") {
			all := make([]Strategy, 0, len(constructors))
			for _, key := range Keys() {
				all = append(all, constructors[key]())
			}
			return all, nil
		}
	}

	var out []Strategy
	for _, k := range keys {
		make, ok := constructors[strings.ToLower(strings.TrimSpace(k))]
		if !ok {
			return nil, fmt.Errorf("unknown strategy %q (available: %s, all)", k, strings.Join(Keys(), ", "))
		}
		out = append(out, make())
	}
	return out, nil
}
