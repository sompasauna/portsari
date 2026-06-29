// Package schedule provides time-window evaluation for door unlock tiers.
package schedule

import (
	"fmt"
	"strings"
	"time"
)

func parseHHMM(s string) (int, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("schedule: invalid time format %q", s)
	}
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return 0, 0, fmt.Errorf("schedule: parse %q: %w", s, err)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("schedule: time out of range %q", s)
	}
	return h, m, nil
}

// IsUnlockAllowed checks whether an unlock is permitted for the given tier at the given time.
func IsUnlockAllowed(tier string, now time.Time, loc *time.Location, daytimeStart, daytimeEnd string) bool {
	switch tier {
	case "full":
		return true
	case "daytime":
		startH, startM, err := parseHHMM(daytimeStart)
		if err != nil {
			return false
		}
		endH, endM, err := parseHHMM(daytimeEnd)
		if err != nil {
			return false
		}
		currentH, currentM, _ := now.In(loc).Clock()

		currentMinutes := currentH*60 + currentM
		startMinutes := startH*60 + startM
		endMinutes := endH*60 + endM

		if startMinutes <= endMinutes {
			return currentMinutes >= startMinutes && currentMinutes < endMinutes
		}
		return currentMinutes >= startMinutes || currentMinutes < endMinutes
	default:
		return false
	}
}
