package main

import (
	"context"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/cost"
	"github.com/sifatulrabbi/sifatul-stack/cli/usage"
)

const week = 7 * 24 * time.Hour

type weeklyCost struct {
	Logs   map[string]cost.Log
	Prices func() (cost.PriceBook, error)
}

func (w weeklyCost) attach(ctx context.Context, reports []usage.Report, now time.Time) {
	var book cost.PriceBook
	var bookErr error
	loaded := false
	for i := range reports {
		r := &reports[i]
		log, ok := w.Logs[r.Provider]
		if !ok || r.Error != "" {
			continue
		}
		weekly, ok := r.WeeklyWindow()
		if !ok {
			continue
		}
		if !loaded {
			book, bookErr = w.Prices()
			loaded = true
		}
		if bookErr != nil {
			r.CostError = bookErr.Error()
			continue
		}
		from := weekly.ResetsAt.Add(-week)
		entries, err := log.Entries(ctx, from)
		if err != nil {
			r.CostError = err.Error()
			continue
		}
		summary := cost.Summarize(entries, book, from, now)
		r.Cost = &summary
	}
}
