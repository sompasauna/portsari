package action

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleLocationDefaultsToLocal(t *testing.T) {
	loc, err := scheduleLocation("")
	require.NoError(t, err)
	assert.Equal(t, "Local", loc.String())
}

func TestScheduleLocationUsesConfiguredTimezone(t *testing.T) {
	loc, err := scheduleLocation("Europe/Helsinki")
	require.NoError(t, err)
	assert.Equal(t, "Europe/Helsinki", loc.String())
}

func TestScheduleLocationInvalidTimezoneFallsBackToLocal(t *testing.T) {
	loc, err := scheduleLocation("not/a-zone")
	require.Error(t, err)
	assert.Equal(t, "Local", loc.String())
}
