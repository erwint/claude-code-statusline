package output

import (
	"strings"
	"testing"

	"github.com/erwint/claude-code-statusline/internal/config"
	"github.com/erwint/claude-code-statusline/internal/types"
)

// nativeCfg returns a colorless config so assertions can match on plain text.
func nativeCfg() *config.Config {
	return &config.Config{
		NoColor:         true,
		DisplayMode:     "minimal",
		ShowSessionCost: true,
		ShowEffort:      true,
		ShowPR:          true,
		ShowWorktree:    true,
	}
}

func renderWith(t *testing.T, sess *types.SessionInput, stats *types.TokenStats) string {
	t.Helper()
	var out string
	withConfig(t, nativeCfg(), func() {
		out = FormatStatusLine(sess, types.GitInfo{}, nil, stats, "", "", false, nil)
	})
	return out
}

func TestSessionCostShown(t *testing.T) {
	out := renderWith(t,
		&types.SessionInput{Cost: &types.SessionCost{TotalCostUSD: 1.234}},
		&types.TokenStats{DailyCost: 10, WeeklyCost: 20, MonthlyCost: 30},
	)

	if !strings.Contains(out, "($1.23)") {
		t.Errorf("expected session cost in output, got: %s", out)
	}
	if !strings.Contains(out, "$30.00/m") {
		t.Errorf("session cost should not replace the breakdown, got: %s", out)
	}
}

// A brand-new session has no cost yet — don't show "$0.00".
func TestSessionCostHiddenWhenZero(t *testing.T) {
	out := renderWith(t,
		&types.SessionInput{Cost: &types.SessionCost{TotalCostUSD: 0}},
		&types.TokenStats{DailyCost: 10},
	)
	if strings.Contains(out, "($") {
		t.Errorf("expected no session cost, got: %s", out)
	}
}

// Older Claude Code versions don't send cost at all.
func TestSessionCostAbsentFromPayload(t *testing.T) {
	out := renderWith(t, &types.SessionInput{}, &types.TokenStats{DailyCost: 10})
	if strings.Contains(out, "($") {
		t.Errorf("expected no session cost, got: %s", out)
	}
}

// Session cost stands alone when there's no historical breakdown yet.
func TestSessionCostWithoutBreakdown(t *testing.T) {
	out := renderWith(t,
		&types.SessionInput{Cost: &types.SessionCost{TotalCostUSD: 0.5}},
		&types.TokenStats{},
	)
	if !strings.Contains(out, "($0.50)") {
		t.Errorf("expected standalone session cost, got: %s", out)
	}
}

func TestEffortAndFastModeInModelBadge(t *testing.T) {
	tests := []struct {
		name string
		sess *types.SessionInput
		want string
	}{
		{
			name: "effort level",
			sess: &types.SessionInput{
				Model:  &types.SessionModel{DisplayName: "Opus 5"},
				Effort: &types.Effort{Level: "xhigh"},
			},
			want: "Opus 5 (xhigh)",
		},
		{
			name: "fast mode",
			sess: &types.SessionInput{
				Model:    &types.SessionModel{DisplayName: "Opus 5"},
				FastMode: true,
			},
			want: "Opus 5 ⚡",
		},
		{
			name: "both",
			sess: &types.SessionInput{
				Model:    &types.SessionModel{DisplayName: "Opus 5"},
				FastMode: true,
				Effort:   &types.Effort{Level: "high"},
			},
			want: "Opus 5 ⚡ (high)",
		},
		{
			name: "neither",
			sess: &types.SessionInput{Model: &types.SessionModel{DisplayName: "Opus 5"}},
			want: "Opus 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderWith(t, tt.sess, &types.TokenStats{})
			if !strings.Contains(out, tt.want) {
				t.Errorf("expected %q in output, got: %s", tt.want, out)
			}
		})
	}
}

func TestPullRequestShown(t *testing.T) {
	out := renderWith(t,
		&types.SessionInput{PR: &types.PullRequest{Number: 1234, ReviewState: "approved"}},
		&types.TokenStats{},
	)
	if !strings.Contains(out, "#1234") {
		t.Errorf("expected PR number in output, got: %s", out)
	}
}

func TestPullRequestHiddenWithoutNumber(t *testing.T) {
	out := renderWith(t, &types.SessionInput{PR: &types.PullRequest{}}, &types.TokenStats{})
	if strings.Contains(out, "#") {
		t.Errorf("expected no PR marker, got: %s", out)
	}
}

func TestReviewStateColors(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"approved", colorGreen},
		{"changes_requested", colorRed},
		{"pending", colorGray},
		{"", colorGray},
		{"something_new", colorGray},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			if got, _ := reviewStateColors(tt.state); got != tt.want {
				t.Errorf("state %q: expected %q, got %q", tt.state, tt.want, got)
			}
		})
	}
}

func TestWorktreeShown(t *testing.T) {
	out := renderWith(t,
		&types.SessionInput{Worktree: &types.Worktree{Name: "my-feature"}},
		&types.TokenStats{},
	)
	if !strings.Contains(out, "⑂ my-feature") {
		t.Errorf("expected worktree name in output, got: %s", out)
	}
}

func TestWorktreeAbsent(t *testing.T) {
	out := renderWith(t, &types.SessionInput{}, &types.TokenStats{})
	if strings.Contains(out, "⑂") {
		t.Errorf("expected no worktree marker, got: %s", out)
	}
}
