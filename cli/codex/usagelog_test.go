package codex

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/cost"
)

func usageTestEvent(timestamp string, totals usageTotals) string {
	data, err := json.Marshal(totals)
	if err != nil {
		panic(err)
	}
	return `{"timestamp":"` + timestamp + `","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":` + string(data) + `,"last_token_usage":{"input_tokens":999999,"output_tokens":999999}}}}` + "\n"
}

func usageTestModel(model string) string {
	return `{"type":"turn_context","payload":{"model":"` + model + `"}}` + "\n"
}

func TestUsageLogCountsCumulativeUsage(t *testing.T) {
	since := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	first := usageTotals{Input: 100, Cached: 40, Output: 20, Reasoning: 5, Total: 120}
	second := usageTotals{Input: 160, Cached: 60, Output: 30, Reasoning: 8, Total: 190}
	start := "2026-11-16T00:00:00Z"
	later := "2026-11-16T01:00:00Z"
	entry := func(timestamp time.Time, model string, tokens cost.Tokens) cost.Entry {
		return cost.Entry{Time: timestamp, Model: model, Tokens: tokens}
	}
	initial := entry(since, "unknown", cost.Tokens{Input: 60, CacheRead: 40, Output: 20})
	delta := entry(since.Add(time.Hour), "unknown", cost.Tokens{Input: 40, CacheRead: 20, Output: 10})
	tests := []struct {
		name string
		data string
		want []cost.Entry
	}{
		{
			name: "repeated identical totals are counted once",
			data: usageTestEvent(start, first) + usageTestEvent(later, first),
			want: []cost.Entry{initial},
		},
		{
			name: "two events emit the initial total and the delta",
			data: usageTestEvent(start, first) + usageTestEvent(later, second),
			want: []cost.Entry{initial, delta},
		},
		{
			name: "events before since establish the baseline",
			data: usageTestEvent("2026-11-15T23:00:00Z", first) + usageTestEvent(later, second),
			want: []cost.Entry{delta},
		},
		{
			name: "model switches attribute subsequent deltas to the new model",
			data: usageTestModel("gpt-6.1-sol") + usageTestEvent(start, first) + usageTestModel("gpt-6-astra") + usageTestEvent(later, second),
			want: []cost.Entry{
				entry(since, "gpt-6.1-sol", initial.Tokens),
				entry(since.Add(time.Hour), "gpt-6-astra", delta.Tokens),
			},
		},
		{
			name: "empty model resets attribution to unknown",
			data: usageTestModel("gpt-6.1-sol") + usageTestModel("") + usageTestEvent(start, first),
			want: []cost.Entry{initial},
		},
		{
			name: "counter reset uses the new total and updates the baseline",
			data: usageTestEvent(start, second) + usageTestEvent(later, first) + usageTestEvent("2026-11-16T02:00:00Z", second),
			want: []cost.Entry{
				entry(since, "unknown", cost.Tokens{Input: 100, CacheRead: 60, Output: 30}),
				entry(since.Add(time.Hour), "unknown", initial.Tokens),
				entry(since.Add(2*time.Hour), "unknown", delta.Tokens),
			},
		},
		{
			name: "a decrease in reasoning alone resets every field",
			data: usageTestEvent("2026-11-15T23:00:00Z", first) + usageTestEvent(start, usageTotals{Input: 160, Cached: 60, Output: 30, Reasoning: 1, Total: 190}),
			want: []cost.Entry{entry(since, "unknown", cost.Tokens{Input: 100, CacheRead: 60, Output: 30})},
		},
		{
			name: "cached input is split and reasoning is not added to output",
			data: usageTestEvent(start, usageTotals{Input: 89539, Cached: 54912, CacheWrite: 7, Output: 4508, Reasoning: 463, Total: 94047}),
			want: []cost.Entry{entry(since, "unknown", cost.Tokens{Input: 34627, CacheRead: 54912, CacheWrite5m: 7, Output: 4508})},
		},
		{
			name: "null info null totals and malformed lines are skipped",
			data: "broken JSON\n" +
				`{"timestamp":"2026-11-16T00:00:00Z","type":"event_msg","payload":{"type":"token_count","info":null}}` + "\n" +
				`{"timestamp":"2026-11-16T00:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":null}}}` + "\n" +
				usageTestEvent("invalid", second) + usageTestEvent(start, first),
			want: []cost.Entry{initial},
		},
		{
			name: "zero totals emit nothing",
			data: usageTestEvent(start, usageTotals{}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := UsageLog{FS: fstest.MapFS{
				"2026/11/16/rollout-a.jsonl": &fstest.MapFile{Data: []byte(tt.data), ModTime: since},
			}}
			got, err := log.Entries(context.Background(), since)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUsageLogSkipsOldFilesAndUnmatchedPaths(t *testing.T) {
	since := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	data := []byte(usageTestEvent("2026-11-16T00:00:00Z", usageTotals{Input: 10}))
	log := UsageLog{FS: fstest.MapFS{
		"2026/11/16/rollout-old.jsonl": &fstest.MapFile{Data: data, ModTime: since.Add(-time.Second)},
		"2026/11/16/rollout-new.jsonl": &fstest.MapFile{Data: data, ModTime: since},
		"2026/11/16/other.jsonl":       &fstest.MapFile{Data: data, ModTime: since},
		"rollout-root.jsonl":           &fstest.MapFile{Data: data, ModTime: since},
	}}
	entries, err := log.Entries(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Tokens.Input != 10 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestUsageLogKeepsFileBaselinesAndModelsIndependent(t *testing.T) {
	since := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	data := usageTestEvent("2026-11-16T00:00:00Z", usageTotals{Input: 10})
	log := UsageLog{FS: fstest.MapFS{
		"2026/11/16/rollout-a.jsonl": &fstest.MapFile{Data: []byte(usageTestModel("gpt-6.1-sol") + data), ModTime: since},
		"2026/11/16/rollout-b.jsonl": &fstest.MapFile{Data: []byte(data), ModTime: since},
	}}
	entries, err := log.Entries(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Model != "gpt-6.1-sol" || entries[1].Model != "unknown" || entries[1].Tokens.Input != 10 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestUsageLogContinuesPastOversizedLines(t *testing.T) {
	since := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	data := strings.Repeat("x", 17*1024*1024) + "\n" + usageTestEvent("2026-11-16T00:00:00Z", usageTotals{Input: 10})
	log := UsageLog{FS: fstest.MapFS{
		"2026/11/16/rollout-a.jsonl": &fstest.MapFile{Data: []byte(data), ModTime: since},
	}}
	entries, err := log.Entries(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Tokens.Input != 10 {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestUsageLogRespectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (UsageLog{FS: fstest.MapFS{}}).Entries(ctx, time.Time{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}
