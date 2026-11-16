package cost

import (
	"context"
	"sort"
	"time"
)

type Tokens struct {
	Input        int64 `json:"input"`
	Output       int64 `json:"output"`
	CacheRead    int64 `json:"cache_read"`
	CacheWrite5m int64 `json:"cache_write_5m"`
	CacheWrite1h int64 `json:"cache_write_1h"`
}

func (t Tokens) Add(o Tokens) Tokens {
	return Tokens{
		Input:        t.Input + o.Input,
		Output:       t.Output + o.Output,
		CacheRead:    t.CacheRead + o.CacheRead,
		CacheWrite5m: t.CacheWrite5m + o.CacheWrite5m,
		CacheWrite1h: t.CacheWrite1h + o.CacheWrite1h,
	}
}

func (t Tokens) Total() int64 {
	return t.Input + t.Output + t.CacheRead + t.CacheWrite5m + t.CacheWrite1h
}

type Entry struct {
	Time   time.Time
	Model  string
	Tokens Tokens
}

type Log interface {
	Entries(ctx context.Context, since time.Time) ([]Entry, error)
}

type Price struct {
	Input        float64
	Output       float64
	CacheRead    float64
	CacheWrite5m float64
	CacheWrite1h float64
}

func (p Price) USD(t Tokens) float64 {
	return float64(t.Input)*p.Input +
		float64(t.Output)*p.Output +
		float64(t.CacheRead)*p.CacheRead +
		float64(t.CacheWrite5m)*p.CacheWrite5m +
		float64(t.CacheWrite1h)*p.CacheWrite1h
}

type PriceBook interface {
	Lookup(model string) (Price, bool)
}

type ModelCost struct {
	Model    string  `json:"model"`
	Tokens   Tokens  `json:"tokens"`
	USD      float64 `json:"usd"`
	Priced   bool    `json:"priced"`
	PricedAs string  `json:"priced_as,omitempty"`
}

type aliasedBook struct {
	book    PriceBook
	aliases map[string]string
}

func WithAliases(book PriceBook, aliases map[string]string) PriceBook {
	return aliasedBook{book: book, aliases: aliases}
}

func (b aliasedBook) Lookup(model string) (Price, bool) {
	if target, ok := b.aliases[model]; ok {
		return b.book.Lookup(target)
	}
	return b.book.Lookup(model)
}

func (b aliasedBook) PricedAs(model string) string {
	return b.aliases[model]
}

type aliasReporter interface {
	PricedAs(model string) string
}

type Summary struct {
	From   time.Time   `json:"from"`
	To     time.Time   `json:"to"`
	USD    float64     `json:"usd"`
	Models []ModelCost `json:"models"`
}

func Summarize(entries []Entry, book PriceBook, from, to time.Time) Summary {
	byModel := map[string]Tokens{}
	for _, e := range entries {
		if e.Time.Before(from) || e.Time.After(to) {
			continue
		}
		byModel[e.Model] = byModel[e.Model].Add(e.Tokens)
	}

	s := Summary{From: from, To: to, Models: []ModelCost{}}
	for model, tokens := range byModel {
		mc := ModelCost{Model: model, Tokens: tokens}
		if price, ok := book.Lookup(model); ok {
			if a, ok := book.(aliasReporter); ok {
				mc.PricedAs = a.PricedAs(model)
			}
			mc.Priced = true
			mc.USD = price.USD(tokens)
			s.USD += mc.USD
		}
		s.Models = append(s.Models, mc)
	}
	sort.Slice(s.Models, func(i, j int) bool {
		a, b := s.Models[i], s.Models[j]
		if a.USD != b.USD {
			return a.USD > b.USD
		}
		return a.Model < b.Model
	})
	return s
}
