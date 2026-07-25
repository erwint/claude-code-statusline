// Command updatepricing regenerates pricing.json from Anthropic's published
// pricing documentation.
//
// Anthropic publishes no pricing API — GET /v1/models returns capabilities and
// token limits but no rates — so the source of truth is the docs page, which is
// served as plain markdown with a stable "Model pricing" table:
//
//	https://platform.claude.com/docs/en/about-claude/pricing.md
//
// The table lists display names ("Claude Opus 4.8") rather than API model IDs,
// so names are mapped to IDs mechanically: lowercase the family, replace the
// dot in the version with a dash. Rows carrying an effective date ("through
// August 31, 2026" / "starting September 1, 2026" — used for Claude Sonnet 5's
// introductory pricing) are filtered down to whichever row is in effect today.
//
// Because that's a scraped source, every result is cross-checked against
// LiteLLM's machine-readable price list. Disagreements are reported but not
// fatal: LiteLLM lags new launches, and Anthropic's own page wins.
//
// Prices are accumulated, never overwritten. pricing.json carries a dated
// history per model so that logs are always costed at the rate that applied on
// the day they were written; a price change appends a new period instead of
// rewriting the past. Announced future changes (Claude Sonnet 5's introductory
// rate ending August 31, 2026) are recorded with their exact start date, so
// they take effect on the right day without a regeneration having to land on
// it. A change we learn about only by observing the docs is dated the day this
// tool notices it — run it often enough that "noticed" stays close to "took
// effect".
//
// Usage:
//
//	go run ./scripts/updatepricing            # rewrite pricing.json
//	go run ./scripts/updatepricing -check     # exit 1 if pricing.json is stale
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	docsURL     = "https://platform.claude.com/docs/en/about-claude/pricing.md"
	crossURL    = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"
	outputFile  = "pricing.json"
	sectionHead = "## Model pricing"
	fastHead    = "### Fast mode pricing"
)

var (
	linkRe     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	modelRe    = regexp.MustCompile(`^Claude\s+([A-Za-z]+)\s+([0-9]+(?:\.[0-9]+)?)\b`)
	priceRe    = regexp.MustCompile(`\$([0-9]+(?:\.[0-9]+)?)`)
	fromRe     = regexp.MustCompile(`starting\s+([A-Z][a-z]+\s+\d{1,2},\s+\d{4})`)
	untilRe    = regexp.MustCompile(`through\s+([A-Z][a-z]+\s+\d{1,2},\s+\d{4})`)
	dateLayout = "January 2, 2006"
)

type entry struct {
	id      string
	family  string
	version float64
	input   float64
	output  float64
	from    time.Time // zero = always
	until   time.Time // zero = always
}

func main() {
	check := flag.Bool("check", false, "exit non-zero if pricing.json is out of date instead of writing it")
	flag.Parse()

	md, err := fetch(docsURL)
	if err != nil {
		fail("fetch pricing docs: %v", err)
	}

	entries, err := parse(md)
	if err != nil {
		fail("parse pricing table: %v", err)
	}
	if len(entries) < 5 {
		fail("parsed only %d models — the docs table format likely changed", len(entries))
	}

	fast, ok := parseFast(md)
	if !ok {
		fail("fast mode pricing section %q not found — the docs format likely changed", fastHead)
	}

	now := time.Now().UTC()
	existing, _ := os.ReadFile(outputFile)
	history := merge(seed(loadHistory(existing)), build(entries, fast, now), now)
	crossCheck(currentPrices(history, now))
	out := render(history, now)

	if bytes.Equal(normalize(existing), normalize(out)) {
		fmt.Printf("%s is up to date (%d models)\n", outputFile, len(history))
		return
	}

	if *check {
		fail("%s is out of date — run: go run ./scripts/updatepricing", outputFile)
	}

	if err := os.WriteFile(outputFile, out, 0644); err != nil {
		fail("write %s: %v", outputFile, err)
	}
	fmt.Printf("wrote %s (%d models)\n", outputFile, len(history))
}

func fetch(url string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

// parse extracts the base input/output prices from the "Model pricing" table.
func parse(md string) ([]entry, error) {
	start := strings.Index(md, sectionHead)
	if start < 0 {
		return nil, fmt.Errorf("section %q not found", sectionHead)
	}
	section := md[start+len(sectionHead):]
	if end := strings.Index(section, "\n## "); end > 0 {
		section = section[:end]
	}

	var entries []entry
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "---") {
			continue
		}
		cells := splitRow(line)
		// Model | Base Input | 5m Cache Writes | 1h Cache Writes | Cache Hits | Output
		if len(cells) < 6 {
			continue
		}
		name := linkRe.ReplaceAllString(cells[0], "$1")
		m := modelRe.FindStringSubmatch(name)
		if m == nil {
			continue // header row or a note
		}
		input, ok1 := price(cells[1])
		output, ok2 := price(cells[5])
		if !ok1 || !ok2 {
			continue
		}
		version, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		e := entry{
			id:      "claude-" + strings.ToLower(m[1]) + "-" + strings.ReplaceAll(m[2], ".", "-"),
			family:  "claude-" + strings.ToLower(m[1]),
			version: version,
			input:   input,
			output:  output,
		}
		qualifier := name[len(m[0]):]
		if d := fromRe.FindStringSubmatch(qualifier); d != nil {
			e.from, _ = time.Parse(dateLayout, d[1])
		}
		if d := untilRe.FindStringSubmatch(qualifier); d != nil {
			if t, err := time.Parse(dateLayout, d[1]); err == nil {
				e.until = t.AddDate(0, 0, 1) // "through Aug 31" == valid until Sep 1
			}
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// parseFast reads the fast-mode rate table. Its rows list several models in one
// cell ("Claude Opus 5 / Claude Opus 4.8"), so each name is resolved
// separately. Models absent from the table have no fast mode.
// The bool return distinguishes "the table says this model has no fast mode"
// from "the table wasn't found". The first is real information — fast mode was
// withdrawn from Opus 4.7 once — and gets recorded. The second would silently
// erase every fast rate we know, so the caller treats it as fatal.
func parseFast(md string) (map[string]modelPrice, bool) {
	out := make(map[string]modelPrice)

	start := strings.Index(md, fastHead)
	if start < 0 {
		return out, false
	}
	section := md[start+len(fastHead):]
	if end := strings.Index(section, "\n#"); end > 0 {
		section = section[:end]
	}

	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || strings.Contains(line, "---") {
			continue
		}
		cells := splitRow(line)
		if len(cells) < 3 {
			continue
		}
		input, ok1 := price(cells[1])
		output, ok2 := price(cells[2])
		if !ok1 || !ok2 {
			continue
		}
		names := linkRe.ReplaceAllString(cells[0], "$1")
		for _, name := range strings.Split(names, "/") {
			m := modelRe.FindStringSubmatch(strings.TrimSpace(name))
			if m == nil {
				continue
			}
			id := "claude-" + strings.ToLower(m[1]) + "-" + strings.ReplaceAll(m[2], ".", "-")
			out[id] = modelPrice{Input: input, Output: output}
		}
	}
	return out, true
}

func splitRow(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func price(cell string) (float64, bool) {
	m := priceRe.FindStringSubmatch(cell)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	return v, err == nil
}

type modelPrice struct {
	Input      float64
	Output     float64
	FastInput  float64
	FastOutput float64
}

// period is one price and the date it took effect. An empty From means the
// price applies to every date before the next period — used when the earlier
// history isn't known.
type period struct {
	From       string  `json:"from,omitempty"`
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	FastInput  float64 `json:"fast_input,omitempty"`
	FastOutput float64 `json:"fast_output,omitempty"`
}

const dayLayout = "2006-01-02"

// build turns the parsed table rows into per-model periods. A row with an
// explicit start date ("starting September 1, 2026") becomes a dated period; a
// row without one becomes the model's baseline period.
func build(entries []entry, fast map[string]modelPrice, now time.Time) map[string][]period {
	out := make(map[string][]period)
	newest := make(map[string]entry)

	add := func(id string, e entry) {
		p := period{Input: e.input, Output: e.output}
		if !e.from.IsZero() {
			p.From = e.from.Format(dayLayout)
		}
		// Fast-mode rates are listed per model, not per period, so they attach
		// to whatever periods this run produces for that model.
		if f, ok := fast[e.id]; ok {
			p.FastInput, p.FastOutput = f.Input, f.Output
		}
		out[id] = append(out[id], p)
	}

	for _, e := range entries {
		// Skip rows whose window has closed ("through August 31, 2026" once
		// September arrives) — the page keeps them for reference, but treating
		// one as the current rate would undo the change that superseded it.
		if !e.until.IsZero() && !now.Before(e.until) {
			continue
		}

		add(e.id, e)

		// The Claude 4 generation shipped a "-0" alias for its bare major version:
		// "Claude Opus 4" is claude-opus-4-0 as well as claude-opus-4-20250514.
		// Later generations use the bare ID, so don't invent aliases for them.
		if e.version == 4 {
			add(e.id+"-0", e)
		}

		if cur, ok := newest[e.family]; !ok || e.version > cur.version {
			newest[e.family] = e
		}
	}

	// Family fallbacks ("claude-opus") catch model IDs released after this file
	// was generated, e.g. a future claude-opus-5-1. The fallback tracks the
	// newest model in the family, including its dated periods — when a new
	// flagship lands at a different price, that's a real change to what an
	// unknown ID in the family costs.
	for family, e := range newest {
		out[family] = append([]period(nil), out[e.id]...)
	}

	for id := range out {
		sortPeriods(out[id])
	}
	return out
}

// backfill is price history established by research rather than by watching the
// docs page over time. It seeds models whose history predates this tool, so a
// freshly generated pricing.json is identical to the accumulated one.
//
// Sources: Claude Sonnet 5 launched 2026-06-30 with introductory pricing of
// $2/$10 through 2026-08-31 (anthropic.com/news/claude-sonnet-5); Claude Opus 5
// launched 2026-07-24 at $5/$25. Verified against LiteLLM's price-map history:
// no other Anthropic model was repriced between 2026-06-24 and 2026-07-25.
var backfill = map[string][]period{
	"claude-sonnet-5": {{From: "2026-06-30", Input: 2, Output: 10}},

	// Opus 5 and Opus 4.8 are the two models with fast mode, at $10/$50 — both
	// have had it since launch, so the rate isn't dated from when this tool
	// first scraped it. Opus 4.8 predates the tracked window, hence no date.
	"claude-opus-5":   {{From: "2026-07-24", Input: 5, Output: 25, FastInput: 10, FastOutput: 50}},
	"claude-opus-4-8": {{Input: 5, Output: 25, FastInput: 10, FastOutput: 50}},

	// Before Sonnet 5 launched, the newest Sonnet was 4.6 at $3/$15 — so an
	// unknown "claude-sonnet-*" ID logged before 2026-06-30 cost $3/$15, not
	// the introductory rate.
	"claude-sonnet": {
		{Input: 3, Output: 15},
		{From: "2026-06-30", Input: 2, Output: 10},
	},
}

// seed inserts researched history for models the file doesn't know about yet.
// It never overrides what's already recorded — accumulated history wins.
func seed(history map[string][]period) map[string][]period {
	for id, periods := range backfill {
		if _, ok := history[id]; !ok {
			history[id] = append([]period(nil), periods...)
		}
	}
	return history
}

// merge folds the freshly scraped periods into the history already on disk.
//
// Nothing already recorded for a past date is ever rewritten — that would
// retroactively re-cost logs that were priced correctly at the time. Only three
// things happen: an unseen model is seeded, an announced future change is
// recorded (or corrected, while still in the future), and a price that differs
// from what we last recorded is appended as a new period starting today.
func merge(old, fresh map[string][]period, now time.Time) map[string][]period {
	today := now.Format(dayLayout)
	merged := make(map[string][]period, len(old)+len(fresh))

	// Models that dropped off the pricing page (retired) keep their history:
	// logs from before the retirement still need to be costed.
	for id, periods := range old {
		merged[id] = append([]period(nil), periods...)
	}

	for id, freshPeriods := range fresh {
		history := merged[id]

		// A model we've never seen: record it as-is, with no start date on the
		// baseline period so earlier logs don't fall off a cliff.
		if len(history) == 0 {
			merged[id] = append([]period(nil), freshPeriods...)
			continue
		}

		// Drop future periods we previously announced but that are no longer on
		// the page — an introductory rate that got extended, say. Past periods
		// are immutable.
		kept := history[:0]
		for _, p := range history {
			if p.From > today && !hasStart(freshPeriods, p.From) {
				continue
			}
			kept = append(kept, p)
		}
		history = kept

		for _, f := range freshPeriods {
			switch {
			case f.From == "":
				// The rate in effect now. Append only if it differs from what
				// we already believe applies today.
				fresh := modelPrice{f.Input, f.Output, f.FastInput, f.FastOutput}
				if cur, ok := priceOn(history, today); !ok || cur != fresh {
					history = append(history, period{
						From: today, Input: f.Input, Output: f.Output,
						FastInput: f.FastInput, FastOutput: f.FastOutput,
					})
				}
			case f.From <= today:
				// A dated change that has already taken effect.
				if !hasStart(history, f.From) {
					history = append(history, f)
				}
			default:
				// Announced for the future: record it, or correct it if the
				// announced price changed before taking effect.
				if i := indexOfStart(history, f.From); i >= 0 {
					history[i] = f
				} else {
					history = append(history, f)
				}
			}
		}

		sortPeriods(history)
		merged[id] = history
	}
	return merged
}

func sortPeriods(periods []period) {
	sort.SliceStable(periods, func(i, j int) bool { return periods[i].From < periods[j].From })
}

func hasStart(periods []period, from string) bool {
	return indexOfStart(periods, from) >= 0
}

func indexOfStart(periods []period, from string) int {
	for i, p := range periods {
		if p.From == from {
			return i
		}
	}
	return -1
}

// priceOn returns the price in effect on day, mirroring the statusline's own
// resolution in internal/cost.
func priceOn(periods []period, day string) (modelPrice, bool) {
	var best period
	found := false
	for _, p := range periods {
		if p.From == "" || p.From <= day {
			best, found = p, true
		}
	}
	if !found {
		return modelPrice{}, false
	}
	return modelPrice{
		Input:      best.Input,
		Output:     best.Output,
		FastInput:  best.FastInput,
		FastOutput: best.FastOutput,
	}, true
}

func currentPrices(history map[string][]period, now time.Time) map[string]modelPrice {
	today := now.Format(dayLayout)
	out := make(map[string]modelPrice, len(history))
	for id, periods := range history {
		if p, ok := priceOn(periods, today); ok {
			out[id] = p
		}
	}
	return out
}

// loadHistory reads the history already committed to pricing.json. A missing or
// unreadable file starts from scratch rather than failing — but a file that
// exists and can't be parsed is fatal, since silently discarding recorded
// history would re-date every price to today.
func loadHistory(data []byte) map[string][]period {
	if len(data) == 0 {
		return map[string][]period{}
	}
	var file struct {
		History map[string][]period `json:"history"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		fail("parse existing %s: %v (refusing to discard recorded price history)", outputFile, err)
	}
	if file.History == nil {
		return map[string][]period{}
	}
	for id := range file.History {
		sortPeriods(file.History[id])
	}
	return file.History
}

// crossCheck compares the scraped prices against LiteLLM's price list and warns
// on any disagreement. It never fails the run — LiteLLM lags new launches, and
// it tracks introductory pricing on its own schedule.
func crossCheck(models map[string]modelPrice) {
	body, err := fetch(crossURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cross-check skipped (%v)\n", err)
		return
	}
	var ref map[string]struct {
		Provider   string  `json:"litellm_provider"`
		InputCost  float64 `json:"input_cost_per_token"`
		OutputCost float64 `json:"output_cost_per_token"`
	}
	if err := json.Unmarshal([]byte(body), &ref); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cross-check skipped (%v)\n", err)
		return
	}

	ids := make([]string, 0, len(models))
	for id := range models {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		r, ok := ref[id]
		if !ok || r.Provider != "anthropic" {
			continue // family fallback key, or a model LiteLLM hasn't picked up
		}
		// Compare base rates only — LiteLLM's map has no fast-mode column.
		got, want := models[id], modelPrice{Input: r.InputCost * 1e6, Output: r.OutputCost * 1e6}
		if got.Input != want.Input || got.Output != want.Output {
			fmt.Fprintf(os.Stderr,
				"warning: %s — docs say $%s/$%s, LiteLLM says $%s/$%s (using docs)\n",
				id, num(got.Input), num(got.Output), num(want.Input), num(want.Output))
		}
	}
}

func render(history map[string][]period, now time.Time) []byte {
	models := currentPrices(history, now)

	ids := make([]string, 0, len(history))
	for id := range history {
		ids = append(ids, id)
	}
	// Group by family, newest first, with the bare family fallback leading.
	sort.Slice(ids, func(i, j int) bool {
		fi, fj := familyOf(ids[i]), familyOf(ids[j])
		if fi != fj {
			return fi < fj
		}
		return ids[i] < ids[j]
	})

	width := 0
	for _, id := range ids {
		if n := len(id) + 3; n > width {
			width = n
		}
	}

	var b bytes.Buffer
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"updated\": %q,\n", now.Format(time.RFC3339))
	fmt.Fprintf(&b, "  \"source\": %q,\n", docsURL)
	b.WriteString("  \"_note\": \"Generated by scripts/updatepricing — do not edit by hand. Prices are USD per million tokens; cache writes and reads are derived from these (see internal/cost). 'history' is authoritative: each entry applies from its date until the next one, so past usage keeps the price it was charged at. 'models' is the rate in effect today, for older statusline versions that predate history.\",\n")

	b.WriteString("  \"models\": {\n")
	for i, id := range ids {
		m := models[id]
		fmt.Fprintf(&b, "    %-*s %s%s\n", width, fmt.Sprintf("%q:", id),
			renderPrice(m.Input, m.Output, m.FastInput, m.FastOutput), comma(i, len(ids)))
	}
	b.WriteString("  },\n")

	b.WriteString("  \"history\": {\n")
	for i, id := range ids {
		periods := history[id]
		if len(periods) == 1 {
			fmt.Fprintf(&b, "    %-*s [%s]%s\n", width, fmt.Sprintf("%q:", id),
				renderPeriod(periods[0]), comma(i, len(ids)))
			continue
		}
		fmt.Fprintf(&b, "    %q: [\n", id)
		for j, p := range periods {
			fmt.Fprintf(&b, "      %s%s\n", renderPeriod(p), comma(j, len(periods)))
		}
		fmt.Fprintf(&b, "    ]%s\n", comma(i, len(ids)))
	}
	b.WriteString("  }\n}\n")
	return b.Bytes()
}

func renderPeriod(p period) string {
	body := renderPrice(p.Input, p.Output, p.FastInput, p.FastOutput)
	if p.From == "" {
		return body
	}
	return fmt.Sprintf("{\"from\": %q, %s", p.From, strings.TrimPrefix(body, "{"))
}

func renderPrice(input, output, fastInput, fastOutput float64) string {
	s := fmt.Sprintf("{\"input\": %s, \"output\": %s", num(input), num(output))
	if fastInput > 0 || fastOutput > 0 {
		s += fmt.Sprintf(", \"fast_input\": %s, \"fast_output\": %s", num(fastInput), num(fastOutput))
	}
	return s + "}"
}

func comma(i, n int) string {
	if i == n-1 {
		return ""
	}
	return ","
}

func familyOf(id string) string {
	parts := strings.SplitN(id, "-", 3)
	if len(parts) < 2 {
		return id
	}
	return parts[0] + "-" + parts[1]
}

func num(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// normalize drops the volatile "updated" timestamp so -check only reports real
// price changes.
func normalize(b []byte) []byte {
	var out [][]byte
	for _, line := range bytes.Split(b, []byte("\n")) {
		if bytes.Contains(line, []byte(`"updated"`)) {
			continue
		}
		out = append(out, line)
	}
	return bytes.Join(out, []byte("\n"))
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "updatepricing: "+format+"\n", args...)
	os.Exit(1)
}
