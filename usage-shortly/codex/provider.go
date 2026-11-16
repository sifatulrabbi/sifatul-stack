package codex

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/usage-shortly/usage"
)

type Reading struct {
	Snapshots             []Snapshot
	Source                string
	AsOf                  time.Time
	ResetCreditsAvailable *int
}

type SnapshotSource interface {
	Read(ctx context.Context) (Reading, error)
}

var ErrNoSnapshot = errors.New("codex: no rate-limit snapshot available")

type Provider struct {
	Live     SnapshotSource
	Fallback SnapshotSource
}

func (p Provider) Name() string { return "codex" }

func (p Provider) Usage(ctx context.Context) (usage.Report, error) {
	reading, liveErr := p.Live.Read(ctx)
	if liveErr != nil {
		if p.Fallback == nil {
			return usage.Report{}, liveErr
		}
		var fbErr error
		reading, fbErr = p.Fallback.Read(ctx)
		if fbErr != nil {
			return usage.Report{}, fmt.Errorf("%w; fallback: %w", liveErr, fbErr)
		}
	}
	if len(reading.Snapshots) == 0 {
		return usage.Report{}, ErrNoSnapshot
	}

	r := ToReport(reading.Snapshots)
	r.Source = reading.Source
	if !reading.AsOf.IsZero() {
		asOf := reading.AsOf
		r.AsOf = &asOf
	}
	if liveErr != nil {
		r.Stale = true
		r.SetExtra("live_error", liveErr.Error())
	}
	if reading.ResetCreditsAvailable != nil {
		r.SetExtra("reset_credits_available", strconv.Itoa(*reading.ResetCreditsAvailable))
	}
	return r, nil
}
