package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"strings"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/cost"
)

type TranscriptLog struct {
	FS fs.FS
}

type transcriptLine struct {
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"requestId"`
	Message   *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			Input         int64 `json:"input_tokens"`
			Output        int64 `json:"output_tokens"`
			CacheRead     int64 `json:"cache_read_input_tokens"`
			CacheCreation int64 `json:"cache_creation_input_tokens"`
			CacheTiers    *struct {
				FiveMin int64 `json:"ephemeral_5m_input_tokens"`
				OneHour int64 `json:"ephemeral_1h_input_tokens"`
			} `json:"cache_creation"`
		} `json:"usage"`
	} `json:"message"`
}

const syntheticModel = "<synthetic>"

func (l TranscriptLog) Entries(ctx context.Context, since time.Time) ([]cost.Entry, error) {
	var entries []cost.Entry
	seen := map[string]int{}
	err := fs.WalkDir(l.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(since) {
			return nil
		}
		return l.scan(path, since, &entries, seen)
	})
	return entries, err
}

func (l TranscriptLog) scan(path string, since time.Time, entries *[]cost.Entry, seen map[string]int) error {
	f, err := l.FS.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"usage"`)) {
			record(line, since, entries, seen)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func record(line []byte, since time.Time, entries *[]cost.Entry, seen map[string]int) {
	var t transcriptLine
	if json.Unmarshal(line, &t) != nil || t.Message == nil || t.Message.Usage == nil {
		return
	}
	if t.Message.Model == syntheticModel || t.Timestamp.Before(since) {
		return
	}
	e := cost.Entry{Time: t.Timestamp, Model: t.Message.Model, Tokens: tokensOf(t)}

	key := t.Message.ID + "|" + t.RequestID
	if key == "|" {
		*entries = append(*entries, e)
		return
	}
	if i, dup := seen[key]; dup {
		if e.Tokens.Output > (*entries)[i].Tokens.Output {
			(*entries)[i] = e
		}
		return
	}
	seen[key] = len(*entries)
	*entries = append(*entries, e)
}

func tokensOf(t transcriptLine) cost.Tokens {
	u := t.Message.Usage
	tokens := cost.Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite5m: u.CacheCreation}
	if u.CacheTiers != nil && u.CacheTiers.FiveMin+u.CacheTiers.OneHour == u.CacheCreation {
		tokens.CacheWrite5m = u.CacheTiers.FiveMin
		tokens.CacheWrite1h = u.CacheTiers.OneHour
	}
	return tokens
}
