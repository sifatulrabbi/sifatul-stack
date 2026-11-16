package claude

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/usage-shortly/cost"
)

var weekStart = time.Date(2026, 11, 12, 19, 0, 0, 0, time.UTC)

func assistant(ts, id, req, model, usage string) string {
	return `{"type":"assistant","timestamp":"` + ts + `","requestId":"` + req + `","message":{"id":"` + id + `","model":"` + model + `","usage":` + usage + `}}` + "\n"
}

func file(content string, mod time.Time) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content), ModTime: mod}
}

func entries(t *testing.T, fsys fstest.MapFS) []cost.Entry {
	t.Helper()
	got, err := TranscriptLog{FS: fsys}.Entries(context.Background(), weekStart)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestTranscriptSplitsCacheWritesByTier(t *testing.T) {
	got := entries(t, fstest.MapFS{"p/s.jsonl": file(assistant("2026-11-16T04:01:29.641Z", "m1", "r1", "claude-opus-5-5",
		`{"input_tokens":2,"cache_creation_input_tokens":676,"cache_read_input_tokens":163921,"output_tokens":193,"cache_creation":{"ephemeral_1h_input_tokens":600,"ephemeral_5m_input_tokens":76}}`), weekStart.Add(time.Hour))})

	want := cost.Tokens{Input: 2, Output: 193, CacheRead: 163921, CacheWrite5m: 76, CacheWrite1h: 600}
	if len(got) != 1 || got[0].Tokens != want || got[0].Model != "claude-opus-5-5" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestTranscriptWithoutTierBreakdownCountsCacheWritesAs5m(t *testing.T) {
	got := entries(t, fstest.MapFS{"p/s.jsonl": file(assistant("2026-11-16T04:00:00Z", "m1", "r1", "claude-sonnet-5-5",
		`{"input_tokens":1,"cache_creation_input_tokens":50,"output_tokens":2}`), weekStart.Add(time.Hour))})

	if len(got) != 1 || got[0].Tokens.CacheWrite5m != 50 || got[0].Tokens.CacheWrite1h != 0 {
		t.Fatalf("entries = %+v", got)
	}
}

func TestTranscriptDeduplicatesRepeatedMessageLinesKeepingFullestUsage(t *testing.T) {
	partial := `{"input_tokens":5,"output_tokens":1}`
	full := `{"input_tokens":5,"output_tokens":40}`
	got := entries(t, fstest.MapFS{
		"p/a.jsonl": file(assistant("2026-11-16T04:00:00Z", "m1", "r1", "claude-opus-5-5", partial)+
			assistant("2026-11-16T04:00:01Z", "m1", "r1", "claude-opus-5-5", full), weekStart.Add(time.Hour)),
		"p/sub/agent.jsonl": file(assistant("2026-11-16T04:00:01Z", "m1", "r1", "claude-opus-5-5", full), weekStart.Add(time.Hour)),
	})

	if len(got) != 1 || got[0].Tokens.Output != 40 {
		t.Fatalf("entries = %+v", got)
	}
}

func TestTranscriptSkipsSyntheticOldAndMalformedLines(t *testing.T) {
	u := `{"input_tokens":1,"output_tokens":1}`
	got := entries(t, fstest.MapFS{
		"p/new.jsonl": file(
			assistant("2026-11-16T04:00:00Z", "m1", "r1", "<synthetic>", u)+
				assistant("2026-11-10T04:00:00Z", "m2", "r2", "claude-opus-5-5", u)+
				`{"usage": not json`+"\n"+
				`{"type":"user","message":{"content":"mentions \"usage\" in text"}}`+"\n"+
				assistant("2026-11-16T05:00:00Z", "m3", "r3", "claude-haiku-5-5", u),
			weekStart.Add(time.Hour)),
		"p/stale.jsonl": file(assistant("2026-11-16T04:00:00Z", "m4", "r4", "claude-opus-5-5", u), weekStart.Add(-time.Hour)),
		"p/notes.txt":   file(assistant("2026-11-16T04:00:00Z", "m5", "r5", "claude-opus-5-5", u), weekStart.Add(time.Hour)),
	})

	if len(got) != 1 || got[0].Model != "claude-haiku-5-5" {
		t.Fatalf("entries = %+v", got)
	}
}
