package session

import (
	"testing"

	"github.com/erwint/claude-code-statusline/internal/types"
)

func TestGetContextPercent(t *testing.T) {
	tests := []struct {
		name     string
		session  *types.SessionInput
		expected float64
	}{
		{
			name:     "nil session",
			session:  nil,
			expected: 0,
		},
		{
			name:     "nil context window",
			session:  &types.SessionInput{},
			expected: 0,
		},
		{
			name: "native percentage available",
			session: &types.SessionInput{
				ContextWindow: &types.ContextWindow{
					Size:           200000,
					UsedPercentage: ptrFloat(42.5),
				},
			},
			expected: 42.5,
		},
		{
			name: "native percentage clamped to 0",
			session: &types.SessionInput{
				ContextWindow: &types.ContextWindow{
					Size:           200000,
					UsedPercentage: ptrFloat(-5.0),
				},
			},
			expected: 0,
		},
		{
			name: "native percentage clamped to 100",
			session: &types.SessionInput{
				ContextWindow: &types.ContextWindow{
					Size:           200000,
					UsedPercentage: ptrFloat(150.0),
				},
			},
			expected: 100,
		},
		{
			name: "calculated from token counts",
			session: &types.SessionInput{
				ContextWindow: &types.ContextWindow{
					Size: 200000,
					CurrentUsage: &types.ContextUsage{
						InputTokens:              80000,
						CacheCreationInputTokens: 10000,
						CacheReadInputTokens:     10000,
					},
				},
			},
			expected: 50, // (80000+10000+10000)/200000 * 100 = 50%
		},
		{
			name: "calculated percentage clamped to 100",
			session: &types.SessionInput{
				ContextWindow: &types.ContextWindow{
					Size: 100000,
					CurrentUsage: &types.ContextUsage{
						InputTokens: 150000,
					},
				},
			},
			expected: 100,
		},
		{
			name: "zero size returns 0",
			session: &types.SessionInput{
				ContextWindow: &types.ContextWindow{
					Size: 0,
					CurrentUsage: &types.ContextUsage{
						InputTokens: 50000,
					},
				},
			},
			expected: 0,
		},
		{
			name: "nil current usage returns 0",
			session: &types.SessionInput{
				ContextWindow: &types.ContextWindow{
					Size:         200000,
					CurrentUsage: nil,
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetContextPercent(tt.session)
			if result != tt.expected {
				t.Errorf("GetContextPercent() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func ptrFloat(f float64) *float64 {
	return &f
}
