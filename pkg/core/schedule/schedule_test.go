package schedule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsUnlockAllowed_full(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
	}{
		{"any time", time.Date(2026, 6, 25, 3, 0, 0, 0, time.UTC)},
		{"ignores window params", time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, IsUnlockAllowed("full", tt.now, time.UTC, "08:00", "22:00"))
		})
	}
}

func TestIsUnlockAllowed_daytime(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"inside window", time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC), true},
		{"before window", time.Date(2026, 6, 25, 7, 59, 0, 0, time.UTC), false},
		{"at start inclusive", time.Date(2026, 6, 25, 8, 0, 0, 0, time.UTC), true},
		{"at end exclusive", time.Date(2026, 6, 25, 22, 0, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUnlockAllowed("daytime", tt.now, time.UTC, "08:00", "22:00")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsUnlockAllowed_daytime_wraparound(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"midnight inside", time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC), true},
		{"before start", time.Date(2026, 6, 25, 21, 0, 0, 0, time.UTC), false},
		{"after end", time.Date(2026, 6, 25, 7, 0, 0, 0, time.UTC), false},
		{"at start inclusive", time.Date(2026, 6, 25, 22, 0, 0, 0, time.UTC), true},
		{"at end exclusive", time.Date(2026, 6, 25, 6, 0, 0, 0, time.UTC), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUnlockAllowed("daytime", tt.now, time.UTC, "22:00", "06:00")
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsUnlockAllowed_unknown(t *testing.T) {
	assert.False(t, IsUnlockAllowed("unknown", time.Now(), time.UTC, "08:00", "22:00"))
}

func TestIsUnlockAllowed_timezones(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	hel, err := time.LoadLocation("Europe/Helsinki")
	require.NoError(t, err)

	tests := []struct {
		name string
		now  time.Time
		loc  *time.Location
		want bool
	}{
		{
			name: "NY winter inside window",
			now:  time.Date(2026, 1, 15, 14, 0, 0, 0, time.UTC), // 09:00 EST
			loc:  ny,
			want: true,
		},
		{
			name: "NY summer inside window",
			now:  time.Date(2026, 7, 15, 14, 0, 0, 0, time.UTC), // 10:00 EDT
			loc:  ny,
			want: true,
		},
		{
			name: "Helsinki winter at start boundary",
			now:  time.Date(2026, 1, 15, 6, 0, 0, 0, time.UTC), // 08:00 EET
			loc:  hel,
			want: true,
		},
		{
			name: "Helsinki summer at start boundary",
			now:  time.Date(2026, 7, 15, 5, 0, 0, 0, time.UTC), // 08:00 EEST
			loc:  hel,
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUnlockAllowed("daytime", tt.now, tt.loc, "08:00", "22:00")
			assert.Equal(t, tt.want, got)
		})
	}
}
