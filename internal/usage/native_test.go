package usage

import (
	"testing"
	"time"

	"github.com/erwint/claude-code-statusline/internal/types"
)

func TestFromSessionUsesNativeRateLimits(t *testing.T) {
	fiveHourReset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	sevenDayReset := time.Now().Add(5 * 24 * time.Hour).Truncate(time.Second)

	got := fromSession(&types.SessionInput{
		RateLimits: &types.RateLimits{
			FiveHour: &types.RateLimitWindow{UsedPercentage: 23.5, ResetsAt: fiveHourReset.Unix()},
			SevenDay: &types.RateLimitWindow{UsedPercentage: 41.2, ResetsAt: sevenDayReset.Unix()},
		},
	})

	if got == nil {
		t.Fatal("expected usage from native rate limits, got nil")
	}
	if got.UsagePercent != 23.5 {
		t.Errorf("five-hour: expected 23.5, got %.1f", got.UsagePercent)
	}
	if !got.ResetTime.Equal(fiveHourReset) {
		t.Errorf("five-hour reset: expected %v, got %v", fiveHourReset, got.ResetTime)
	}
	if got.SevenDayPercent != 41.2 {
		t.Errorf("seven-day: expected 41.2, got %.1f", got.SevenDayPercent)
	}
	if !got.SevenDayResetTime.Equal(sevenDayReset) {
		t.Errorf("seven-day reset: expected %v, got %v", sevenDayReset, got.SevenDayResetTime)
	}

	// Native data is live by definition — never flagged stale or unavailable.
	if got.Stale || got.Unavailable {
		t.Error("native rate limits should never be marked stale or unavailable")
	}
}

// Only one window present is still usable.
func TestFromSessionPartialWindows(t *testing.T) {
	got := fromSession(&types.SessionInput{
		RateLimits: &types.RateLimits{
			FiveHour: &types.RateLimitWindow{UsedPercentage: 10},
		},
	})
	if got == nil {
		t.Fatal("expected usage with only the five-hour window")
	}
	if got.UsagePercent != 10 {
		t.Errorf("expected 10, got %.1f", got.UsagePercent)
	}
	if !got.SevenDayResetTime.IsZero() {
		t.Error("absent seven-day window should leave the reset time zero")
	}
}

// Falling back to the API path is signalled by a nil return.
func TestFromSessionFallsBackWhenAbsent(t *testing.T) {
	cases := map[string]*types.SessionInput{
		"nil session":       nil,
		"no rate_limits":    {},
		"empty rate_limits": {RateLimits: &types.RateLimits{}},
	}

	for name, sess := range cases {
		t.Run(name, func(t *testing.T) {
			if got := fromSession(sess); got != nil {
				t.Errorf("expected nil so the API path runs, got %+v", got)
			}
		})
	}
}

// A zero reset timestamp must not become 1970.
func TestFromSessionIgnoresZeroResetTimestamps(t *testing.T) {
	got := fromSession(&types.SessionInput{
		RateLimits: &types.RateLimits{
			FiveHour: &types.RateLimitWindow{UsedPercentage: 5, ResetsAt: 0},
		},
	})
	if got == nil {
		t.Fatal("expected usage")
	}
	if !got.ResetTime.IsZero() {
		t.Errorf("expected zero reset time, got %v", got.ResetTime)
	}
}
