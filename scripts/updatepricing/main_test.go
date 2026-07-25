package main

import (
	"reflect"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse(dayLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

// A model we've never seen gets an undated baseline period, so usage logged
// before we started tracking it is still costed.
func TestMergeSeedsNewModel(t *testing.T) {
	got := merge(
		map[string][]period{},
		map[string][]period{"claude-opus-5": {{Input: 5, Output: 25}}},
		day("2026-07-25"),
	)
	want := []period{{Input: 5, Output: 25}}
	if !reflect.DeepEqual(got["claude-opus-5"], want) {
		t.Errorf("got %+v, want %+v", got["claude-opus-5"], want)
	}
}

// Re-running when nothing changed must not append a redundant period.
func TestMergeIsIdempotent(t *testing.T) {
	history := map[string][]period{"claude-opus-5": {{Input: 5, Output: 25}}}
	fresh := map[string][]period{"claude-opus-5": {{Input: 5, Output: 25}}}

	once := merge(history, fresh, day("2026-07-25"))
	twice := merge(once, fresh, day("2026-08-01"))

	if len(twice["claude-opus-5"]) != 1 {
		t.Errorf("expected 1 period, got %+v", twice["claude-opus-5"])
	}
}

// The important one: an observed price change is recorded from the day it was
// noticed, and the previous price is left untouched so earlier days keep it.
func TestMergeAppendsRatherThanRewrites(t *testing.T) {
	history := map[string][]period{"claude-opus-5": {{Input: 5, Output: 25}}}
	fresh := map[string][]period{"claude-opus-5": {{Input: 6, Output: 30}}}

	got := merge(history, fresh, day("2026-09-15"))

	want := []period{
		{Input: 5, Output: 25},
		{From: "2026-09-15", Input: 6, Output: 30},
	}
	if !reflect.DeepEqual(got["claude-opus-5"], want) {
		t.Fatalf("got %+v, want %+v", got["claude-opus-5"], want)
	}

	if p, _ := priceOn(got["claude-opus-5"], "2026-09-14"); p.Input != 5 {
		t.Errorf("day before the change should still price at 5, got %.2f", p.Input)
	}
	if p, _ := priceOn(got["claude-opus-5"], "2026-09-15"); p.Input != 6 {
		t.Errorf("day of the change should price at 6, got %.2f", p.Input)
	}
}

// An announced change is stored with its real start date, and stays put as it
// crosses over from future to past — including once the superseded row drops
// off the page.
func TestMergeAnnouncedChangeSurvivesCutover(t *testing.T) {
	fresh := map[string][]period{"claude-sonnet-5": {
		{Input: 2, Output: 10},
		{From: "2026-09-01", Input: 3, Output: 15},
	}}

	before := merge(map[string][]period{}, fresh, day("2026-07-25"))

	// On the cutover date the docs list only the standard rate.
	after := merge(before,
		map[string][]period{"claude-sonnet-5": {{From: "2026-09-01", Input: 3, Output: 15}}},
		day("2026-09-01"),
	)

	want := []period{
		{Input: 2, Output: 10},
		{From: "2026-09-01", Input: 3, Output: 15},
	}
	if !reflect.DeepEqual(after["claude-sonnet-5"], want) {
		t.Fatalf("got %+v, want %+v", after["claude-sonnet-5"], want)
	}
	if p, _ := priceOn(after["claude-sonnet-5"], "2026-08-15"); p.Input != 2 {
		t.Errorf("August should keep the introductory rate, got %.2f", p.Input)
	}
}

// A future rate that gets revised before taking effect is corrected in place —
// it never applied to anything, so there's nothing to preserve.
func TestMergeCorrectsUnstartedFuturePeriod(t *testing.T) {
	history := map[string][]period{"claude-sonnet-5": {
		{Input: 2, Output: 10},
		{From: "2026-09-01", Input: 3, Output: 15},
	}}
	fresh := map[string][]period{"claude-sonnet-5": {
		{Input: 2, Output: 10},
		{From: "2026-09-01", Input: 4, Output: 20},
	}}

	got := merge(history, fresh, day("2026-07-25"))

	want := []period{
		{Input: 2, Output: 10},
		{From: "2026-09-01", Input: 4, Output: 20},
	}
	if !reflect.DeepEqual(got["claude-sonnet-5"], want) {
		t.Errorf("got %+v, want %+v", got["claude-sonnet-5"], want)
	}
}

// An introductory rate that gets extended: the announced future period is
// withdrawn from the page and should disappear from the file too.
func TestMergeDropsWithdrawnFuturePeriod(t *testing.T) {
	history := map[string][]period{"claude-sonnet-5": {
		{Input: 2, Output: 10},
		{From: "2026-09-01", Input: 3, Output: 15},
	}}
	fresh := map[string][]period{"claude-sonnet-5": {{Input: 2, Output: 10}}}

	got := merge(history, fresh, day("2026-07-25"))

	want := []period{{Input: 2, Output: 10}}
	if !reflect.DeepEqual(got["claude-sonnet-5"], want) {
		t.Errorf("got %+v, want %+v", got["claude-sonnet-5"], want)
	}
}

// A retired model drops off the pricing page, but logs from before retirement
// still need costing.
func TestMergeKeepsRetiredModels(t *testing.T) {
	history := map[string][]period{"claude-haiku-3-5": {{Input: 0.8, Output: 4}}}
	fresh := map[string][]period{"claude-opus-5": {{Input: 5, Output: 25}}}

	got := merge(history, fresh, day("2026-07-25"))

	if len(got["claude-haiku-3-5"]) != 1 {
		t.Errorf("retired model lost its history: %+v", got["claude-haiku-3-5"])
	}
}

// Researched history seeds a model the file doesn't know yet, and the scraped
// current rate must not then re-date it to today.
func TestSeedThenMergeKeepsResearchedDate(t *testing.T) {
	history := seed(map[string][]period{})
	got := merge(history,
		map[string][]period{"claude-opus-5": {
			{Input: 5, Output: 25, FastInput: 10, FastOutput: 50},
		}},
		day("2026-07-25"),
	)

	want := []period{{From: "2026-07-24", Input: 5, Output: 25, FastInput: 10, FastOutput: 50}}
	if !reflect.DeepEqual(got["claude-opus-5"], want) {
		t.Errorf("got %+v, want %+v", got["claude-opus-5"], want)
	}
}

// Fast mode has been withdrawn from a model before (Opus 4.7), so a model
// dropping out of the fast table is recorded as a dated change, not ignored.
func TestMergeRecordsFastModeWithdrawal(t *testing.T) {
	history := map[string][]period{"claude-opus-4-7": {
		{Input: 5, Output: 25, FastInput: 10, FastOutput: 50},
	}}
	fresh := map[string][]period{"claude-opus-4-7": {{Input: 5, Output: 25}}}

	got := merge(history, fresh, day("2026-08-01"))

	want := []period{
		{Input: 5, Output: 25, FastInput: 10, FastOutput: 50},
		{From: "2026-08-01", Input: 5, Output: 25},
	}
	if !reflect.DeepEqual(got["claude-opus-4-7"], want) {
		t.Fatalf("got %+v, want %+v", got["claude-opus-4-7"], want)
	}

	if p, _ := priceOn(got["claude-opus-4-7"], "2026-07-31"); p.FastInput != 10 {
		t.Error("fast usage before the withdrawal should still bill at the fast rate")
	}
	if p, _ := priceOn(got["claude-opus-4-7"], "2026-08-01"); p.FastInput != 0 {
		t.Error("fast rate should be gone after withdrawal")
	}
}

// Both models sharing one cell of the fast table are resolved separately.
func TestParseFastMultiModelRow(t *testing.T) {
	md := "### Fast mode pricing\n\n" +
		"| Model | Input | Output |\n" +
		"| --- | --- | --- |\n" +
		"| Claude Opus 5 / Claude Opus 4.8 | $10 / MTok | $50 / MTok |\n"

	got, ok := parseFast(md)
	if !ok {
		t.Fatal("expected the section to be found")
	}
	for _, id := range []string{"claude-opus-5", "claude-opus-4-8"} {
		if got[id] != (modelPrice{Input: 10, Output: 50}) {
			t.Errorf("%s: got %+v, want $10/$50", id, got[id])
		}
	}
}

// A missing section must be reported, not treated as "no model has fast mode".
func TestParseFastMissingSection(t *testing.T) {
	if _, ok := parseFast("## Model pricing\n\nnothing here\n"); ok {
		t.Error("expected ok=false when the fast mode section is absent")
	}
}

// Accumulated history outranks the seed table — the file is the record.
func TestSeedDoesNotOverrideRecordedHistory(t *testing.T) {
	recorded := []period{{From: "2026-07-01", Input: 9, Output: 9}}
	got := seed(map[string][]period{"claude-opus-5": recorded})

	if !reflect.DeepEqual(got["claude-opus-5"], recorded) {
		t.Errorf("seed overwrote recorded history: %+v", got["claude-opus-5"])
	}
}

// The Sonnet family fallback repriced when Sonnet 5 launched: an unknown
// sonnet ID logged before 2026-06-30 cost $3/$15, not the introductory rate.
func TestBackfilledSonnetFamilyFallback(t *testing.T) {
	periods := backfill["claude-sonnet"]

	if p, _ := priceOn(periods, "2026-06-29"); p.Input != 3 {
		t.Errorf("before Sonnet 5 launch expected $3, got $%.2f", p.Input)
	}
	if p, _ := priceOn(periods, "2026-06-30"); p.Input != 2 {
		t.Errorf("on Sonnet 5 launch day expected $2, got $%.2f", p.Input)
	}
}

// Expired rows stay on the pricing page for reference; treating one as current
// would undo the change that superseded it.
func TestBuildSkipsExpiredRows(t *testing.T) {
	entries := []entry{
		{id: "claude-sonnet-5", family: "claude-sonnet", version: 5, input: 2, output: 10, until: day("2026-09-01")},
		{id: "claude-sonnet-5", family: "claude-sonnet", version: 5, input: 3, output: 15, from: day("2026-09-01")},
	}

	got := build(entries, nil, day("2026-09-15"))

	want := []period{{From: "2026-09-01", Input: 3, Output: 15}}
	if !reflect.DeepEqual(got["claude-sonnet-5"], want) {
		t.Errorf("got %+v, want %+v", got["claude-sonnet-5"], want)
	}
}
