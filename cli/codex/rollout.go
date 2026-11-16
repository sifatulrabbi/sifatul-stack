package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"regexp"
	"sort"
	"time"
)

type RolloutSource struct {
	FS fs.FS
}

type rolloutWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      *int64  `json:"resets_at"`
}

type rolloutLimits struct {
	LimitID   string         `json:"limit_id"`
	PlanType  string         `json:"plan_type"`
	Primary   *rolloutWindow `json:"primary"`
	Secondary *rolloutWindow `json:"secondary"`
	Credits   struct {
		Balance   string `json:"balance"`
		Unlimited bool   `json:"unlimited"`
	} `json:"credits"`
}

var rolloutPath = regexp.MustCompile(`^[0-9]{4}/[0-9]{2}/[0-9]{2}/rollout-.*\.jsonl$`)

func (s RolloutSource) Read(ctx context.Context) (Reading, error) {
	var paths []string
	err := fs.WalkDir(s.FS, ".", func(name string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() && rolloutPath.MatchString(name) {
			paths = append(paths, name)
		}
		return nil
	})
	if err != nil {
		return Reading{}, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	for _, name := range paths {
		if err := ctx.Err(); err != nil {
			return Reading{}, err
		}
		reading, err := s.readFile(name)
		if err != nil {
			return Reading{}, err
		}
		if len(reading.Snapshots) != 0 {
			return reading, nil
		}
	}
	return Reading{}, ErrNoSnapshot
}

func (s RolloutSource) readFile(name string) (Reading, error) {
	f, err := s.FS.Open(name)
	if err != nil {
		return Reading{}, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	scanner.Split(rolloutLines())
	var latest Reading
	for scanner.Scan() {
		reading, ok := parseRolloutLine(scanner.Bytes())
		if ok && (len(latest.Snapshots) == 0 || reading.AsOf.After(latest.AsOf)) {
			latest = reading
		}
	}
	return latest, scanner.Err()
}

func rolloutLines() bufio.SplitFunc {
	discarding := false
	return func(data []byte, atEOF bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			if discarding {
				discarding = false
				return i + 1, nil, nil
			}
			return i + 1, data[:i], nil
		}
		if discarding || len(data) >= 16*1024*1024 {
			discarding = true
			return len(data), nil, nil
		}
		return bufio.ScanLines(data, atEOF)
	}
}

func parseRolloutLine(data []byte) (Reading, bool) {
	var line struct {
		Timestamp time.Time `json:"timestamp"`
		Type      string    `json:"type"`
		Payload   struct {
			Type       string         `json:"type"`
			RateLimits *rolloutLimits `json:"rate_limits"`
		} `json:"payload"`
	}
	if json.Unmarshal(data, &line) != nil || line.Type != "event_msg" || line.Payload.Type != "token_count" || line.Payload.RateLimits == nil || line.Timestamp.IsZero() {
		return Reading{}, false
	}
	limits := line.Payload.RateLimits
	if limits.LimitID == "" {
		limits.LimitID = "codex"
	}
	snapshot := Snapshot{
		LimitID:          limits.LimitID,
		PlanType:         limits.PlanType,
		Primary:          rolloutSnapshotWindow(limits.Primary),
		Secondary:        rolloutSnapshotWindow(limits.Secondary),
		CreditsBalance:   limits.Credits.Balance,
		CreditsUnlimited: limits.Credits.Unlimited,
	}
	return Reading{Snapshots: []Snapshot{snapshot}, Source: "rollout-cache", AsOf: line.Timestamp}, true
}

func rolloutSnapshotWindow(window *rolloutWindow) *Window {
	if window == nil {
		return nil
	}
	return &Window{UsedPercent: window.UsedPercent, WindowMinutes: window.WindowMinutes, ResetsAt: unixPtr(window.ResetsAt)}
}
