package usersjobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDailyAtUTC(t *testing.T) {
	t.Parallel()

	schedule := dailyAtUTC{hour: 2}
	assert.Equal(t,
		time.Date(2026, time.July, 13, 2, 0, 0, 0, time.UTC),
		schedule.Next(time.Date(2026, time.July, 12, 23, 30, 0, 0, time.FixedZone("west", -2*60*60))),
	)
	assert.Equal(t,
		time.Date(2026, time.July, 14, 2, 0, 0, 0, time.UTC),
		schedule.Next(time.Date(2026, time.July, 13, 2, 0, 0, 0, time.UTC)),
	)
}
