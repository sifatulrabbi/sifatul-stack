package codex

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func rolloutTestLine(timestamp, limits string) string {
	return `{"timestamp":"` + timestamp + `","type":"event_msg","payload":{"type":"token_count","rate_limits":` + limits + `}}` + "\n"
}

func TestRolloutUsesNewestFile(t *testing.T) {
	source := RolloutSource{FS: fstest.MapFS{
		"2026/11/15/rollout-z.jsonl": {Data: []byte(rolloutTestLine("2026-11-17T00:00:00Z", `{"limit_id":"older"}`))},
		"2026/11/16/rollout-a.jsonl": {Data: []byte(rolloutTestLine("2026-11-16T00:00:00Z", `{"limit_id":"earlier-name"}`))},
		"2026/11/16/rollout-z.jsonl": {Data: []byte(rolloutTestLine("2026-11-15T00:00:00Z", `{"limit_id":"newest"}`))},
	}}
	reading, err := source.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(reading.Snapshots) != 1 || reading.Snapshots[0].LimitID != "newest" {
		t.Fatalf("unexpected reading: %+v", reading)
	}
}

func TestRolloutUsesLatestTimestampWithinFile(t *testing.T) {
	data := rolloutTestLine("2026-11-16T03:37:23.770Z", `{"limit_id":"latest"}`) +
		rolloutTestLine("2026-11-16T04:00:00+01:00", `{"limit_id":"last"}`)
	source := RolloutSource{FS: fstest.MapFS{"2026/11/16/rollout-a.jsonl": {Data: []byte(data)}}}
	reading, err := source.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 16, 3, 37, 23, 770000000, time.UTC)
	if reading.Snapshots[0].LimitID != "latest" || !reading.AsOf.Equal(want) {
		t.Fatalf("unexpected reading: %+v", reading)
	}
}

func TestRolloutSkipsNullLimitsAndMalformedLines(t *testing.T) {
	data := "broken JSON\n" + rolloutTestLine("2026-11-17T00:00:00Z", "null") +
		rolloutTestLine("invalid", `{}`) +
		`{"timestamp":"2026-11-17T00:00:00Z","type":"other","payload":{"type":"token_count","rate_limits":{}}}` + "\n" +
		`{"timestamp":"2026-11-17T00:00:00Z","type":"event_msg","payload":{"type":"other","rate_limits":{}}}` + "\n" +
		rolloutTestLine("2026-11-16T00:00:00Z", `{}`)
	source := RolloutSource{FS: fstest.MapFS{"2026/11/16/rollout-a.jsonl": {Data: []byte(data)}}}
	reading, err := source.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if reading.Source != "rollout-cache" || len(reading.Snapshots) != 1 || reading.Snapshots[0].LimitID != "codex" || reading.AsOf.Day() != 16 {
		t.Fatalf("unexpected reading: %+v", reading)
	}
}

func TestRolloutMapsWindowsAndCredits(t *testing.T) {
	data := rolloutTestLine("2026-11-16T03:37:23.770Z", `{"limit_id":"codex","plan_type":"plus","primary":{"used_percent":30.0,"window_minutes":10080,"resets_at":1795042855},"secondary":{"used_percent":2.5,"window_minutes":300,"resets_at":1795042800},"credits":{"balance":"0E-10","unlimited":true}}`)
	source := RolloutSource{FS: fstest.MapFS{"2026/11/16/rollout-a.jsonl": {Data: []byte(data)}}}
	reading, err := source.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reading.Snapshots[0]
	if snapshot.PlanType != "plus" || snapshot.CreditsBalance != "0E-10" || !snapshot.CreditsUnlimited {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.Primary == nil || snapshot.Primary.UsedPercent != 30 || snapshot.Primary.WindowMinutes != 10080 || snapshot.Primary.ResetsAt == nil || !snapshot.Primary.ResetsAt.Equal(time.Unix(1795042855, 0)) {
		t.Fatalf("unexpected primary: %+v", snapshot.Primary)
	}
	if snapshot.Secondary == nil || snapshot.Secondary.UsedPercent != 2.5 || snapshot.Secondary.WindowMinutes != 300 || snapshot.Secondary.ResetsAt == nil || !snapshot.Secondary.ResetsAt.Equal(time.Unix(1795042800, 0)) {
		t.Fatalf("unexpected secondary: %+v", snapshot.Secondary)
	}
}

func TestRolloutHandlesNullSecondaryAndReset(t *testing.T) {
	data := rolloutTestLine("2026-11-16T00:00:00Z", `{"primary":{"used_percent":30,"window_minutes":10080,"resets_at":null},"secondary":null}`)
	source := RolloutSource{FS: fstest.MapFS{"2026/11/16/rollout-a.jsonl": {Data: []byte(data)}}}
	reading, err := source.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := reading.Snapshots[0]
	if snapshot.Primary == nil || snapshot.Primary.ResetsAt != nil || snapshot.Secondary != nil {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestRolloutReturnsNoSnapshotForEmptyTree(t *testing.T) {
	_, err := (RolloutSource{FS: fstest.MapFS{}}).Read(context.Background())
	if !errors.Is(err, ErrNoSnapshot) {
		t.Fatalf("got %v, want ErrNoSnapshot", err)
	}
}

func TestRolloutContinuesPastOversizedLines(t *testing.T) {
	data := strings.Repeat("x", 16*1024*1024+1) + "\n" + rolloutTestLine("2026-11-16T00:00:00Z", `{}`)
	source := RolloutSource{FS: fstest.MapFS{"2026/11/16/rollout-a.jsonl": {Data: []byte(data)}}}
	reading, err := source.Read(context.Background())
	if err != nil || len(reading.Snapshots) != 1 {
		t.Fatalf("reading: %+v, error: %v", reading, err)
	}
}

func TestRolloutFallsBackPastFilesWithoutSnapshots(t *testing.T) {
	source := RolloutSource{FS: fstest.MapFS{
		"2026/11/16/rollout-a.jsonl": {Data: []byte("broken\n")},
		"2026/11/15/rollout-a.jsonl": {Data: []byte(rolloutTestLine("2026-11-15T00:00:00Z", `{}`))},
		"rollout-z.jsonl":            {Data: []byte(rolloutTestLine("2026-11-17T00:00:00Z", `{}`))},
	}}
	reading, err := source.Read(context.Background())
	if err != nil || reading.AsOf.Day() != 15 {
		t.Fatalf("reading: %+v, error: %v", reading, err)
	}
}

func TestRolloutRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (RolloutSource{FS: fstest.MapFS{}}).Read(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}
