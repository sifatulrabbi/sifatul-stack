package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"io/fs"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/cost"
)

type UsageLog struct {
	FS fs.FS
}

var _ cost.Log = UsageLog{}

type usageTotals struct {
	Input      int64 `json:"input_tokens"`
	Cached     int64 `json:"cached_input_tokens"`
	CacheWrite int64 `json:"cache_write_input_tokens"`
	Output     int64 `json:"output_tokens"`
	Reasoning  int64 `json:"reasoning_output_tokens"`
	Total      int64 `json:"total_tokens"`
}

func (u usageTotals) delta(previous usageTotals) usageTotals {
	d := usageTotals{
		Input:      u.Input - previous.Input,
		Cached:     u.Cached - previous.Cached,
		CacheWrite: u.CacheWrite - previous.CacheWrite,
		Output:     u.Output - previous.Output,
		Reasoning:  u.Reasoning - previous.Reasoning,
		Total:      u.Total - previous.Total,
	}
	for _, value := range []int64{d.Input, d.Cached, d.CacheWrite, d.Output, d.Reasoning, d.Total} {
		if value < 0 {
			return u
		}
	}
	return d
}

func (u UsageLog) Entries(ctx context.Context, since time.Time) ([]cost.Entry, error) {
	var entries []cost.Entry
	err := fs.WalkDir(u.FS, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !rolloutPath.MatchString(name) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(since) {
			return nil
		}
		fileEntries, err := u.readUsageFile(name, since)
		if err != nil {
			return err
		}
		entries = append(entries, fileEntries...)
		return nil
	})
	return entries, err
}

func (u UsageLog) readUsageFile(name string, since time.Time) ([]cost.Entry, error) {
	f, err := u.FS.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	scanner.Split(rolloutLines())
	var entries []cost.Entry
	var previous usageTotals
	model := "unknown"
	for scanner.Scan() {
		var line struct {
			Timestamp time.Time `json:"timestamp"`
			Type      string    `json:"type"`
			Payload   struct {
				Model string `json:"model"`
				Type  string `json:"type"`
				Info  *struct {
					Total *usageTotals `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		if line.Type == "turn_context" {
			model = line.Payload.Model
			if model == "" {
				model = "unknown"
			}
			continue
		}
		if line.Type != "event_msg" || line.Payload.Type != "token_count" || line.Payload.Info == nil || line.Payload.Info.Total == nil || line.Timestamp.IsZero() {
			continue
		}
		current := *line.Payload.Info.Total
		delta := current.delta(previous)
		previous = current
		if delta == (usageTotals{}) || line.Timestamp.Before(since) {
			continue
		}
		entries = append(entries, cost.Entry{
			Time:  line.Timestamp,
			Model: model,
			Tokens: cost.Tokens{
				Input:        delta.Input - delta.Cached,
				CacheRead:    delta.Cached,
				CacheWrite5m: delta.CacheWrite,
				Output:       delta.Output,
			},
		})
	}
	return entries, scanner.Err()
}
