package cost

import (
	"testing"
	"time"
)

type book map[string]Price

func (b book) Lookup(model string) (Price, bool) { p, ok := b[model]; return p, ok }

var (
	from = time.Date(2026, 11, 12, 19, 0, 0, 0, time.UTC)
	to   = from.Add(7 * 24 * time.Hour)
)

func TestSummarizePricesEveryTokenClass(t *testing.T) {
	prices := book{"m": {Input: 1, Output: 10, CacheRead: 0.1, CacheWrite5m: 1.25, CacheWrite1h: 2}}
	s := Summarize([]Entry{
		{Time: from.Add(time.Hour), Model: "m", Tokens: Tokens{Input: 2, Output: 3, CacheRead: 10, CacheWrite5m: 4, CacheWrite1h: 5}},
	}, prices, from, to)

	if want := 2.0 + 30 + 1 + 5 + 10; s.USD != want {
		t.Fatalf("usd = %v, want %v", s.USD, want)
	}
}

func TestSummarizeKeepsOnlyEntriesInsideTheWeek(t *testing.T) {
	prices := book{"m": {Output: 1}}
	s := Summarize([]Entry{
		{Time: from.Add(-time.Second), Model: "m", Tokens: Tokens{Output: 100}},
		{Time: from, Model: "m", Tokens: Tokens{Output: 1}},
		{Time: to, Model: "m", Tokens: Tokens{Output: 2}},
		{Time: to.Add(time.Second), Model: "m", Tokens: Tokens{Output: 100}},
	}, prices, from, to)

	if s.USD != 3 {
		t.Fatalf("usd = %v, want 3", s.USD)
	}
}

func TestSummarizeAggregatesPerModelAndListsUnpricedWithoutCost(t *testing.T) {
	prices := book{"cheap": {Output: 1}, "pricey": {Output: 10}}
	at := from.Add(time.Hour)
	s := Summarize([]Entry{
		{Time: at, Model: "cheap", Tokens: Tokens{Output: 5}},
		{Time: at, Model: "pricey", Tokens: Tokens{Output: 1}},
		{Time: at, Model: "cheap", Tokens: Tokens{Output: 2}},
		{Time: at, Model: "mystery", Tokens: Tokens{Input: 50}},
	}, prices, from, to)

	if s.USD != 17 || len(s.Models) != 3 {
		t.Fatalf("summary = %+v", s)
	}
	if s.Models[0].Model != "pricey" || s.Models[1].Model != "cheap" || s.Models[1].Tokens.Output != 7 {
		t.Fatalf("models not aggregated/sorted by cost: %+v", s.Models)
	}
	if m := s.Models[2]; m.Model != "mystery" || m.Priced || m.USD != 0 || m.Tokens.Total() != 50 {
		t.Fatalf("unpriced = %+v", m)
	}
}

func TestAliasPricesModelWithAnotherModelsRateAndSaysSo(t *testing.T) {
	prices := WithAliases(book{"gpt-6.1-terra": {Output: 2}, "gpt-6.1-sol": {Output: 5}}, map[string]string{"codex-auto-review": "gpt-6.1-terra"})
	at := from.Add(time.Hour)
	s := Summarize([]Entry{
		{Time: at, Model: "codex-auto-review", Tokens: Tokens{Output: 10}},
		{Time: at, Model: "gpt-6.1-sol", Tokens: Tokens{Output: 1}},
	}, prices, from, to)

	if s.USD != 25 {
		t.Fatalf("usd = %v, want 25", s.USD)
	}
	review := s.Models[0]
	if review.Model != "codex-auto-review" || !review.Priced || review.PricedAs != "gpt-6.1-terra" {
		t.Fatalf("aliased model = %+v", review)
	}
	if s.Models[1].PricedAs != "" {
		t.Fatalf("unaliased model reports alias: %+v", s.Models[1])
	}
}

func TestAliasToUnknownModelStaysUnpriced(t *testing.T) {
	prices := WithAliases(book{}, map[string]string{"codex-auto-review": "gone"})
	s := Summarize([]Entry{{Time: from, Model: "codex-auto-review", Tokens: Tokens{Output: 1}}}, prices, from, to)
	if s.Models[0].Priced || s.Models[0].PricedAs != "" {
		t.Fatalf("model = %+v", s.Models[0])
	}
}
