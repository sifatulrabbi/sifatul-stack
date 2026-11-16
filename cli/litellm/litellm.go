package litellm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sifatulrabbi/sifatul-stack/cli/cost"
)

const DefaultURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

var ErrNotInstalled = errors.New("litellm prices not installed (run `sifatul-stack usage prices update`)")

type entry struct {
	Provider        string   `json:"litellm_provider"`
	Input           *float64 `json:"input_cost_per_token"`
	Output          *float64 `json:"output_cost_per_token"`
	CacheRead       *float64 `json:"cache_read_input_token_cost"`
	CacheWrite      *float64 `json:"cache_creation_input_token_cost"`
	CacheWriteAbove *float64 `json:"cache_creation_input_token_cost_above_1hr"`
}

type Book map[string]cost.Price

func Parse(raw []byte) (Book, error) {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("litellm: parse prices: %w", err)
	}
	book := Book{}
	for name, rawEntry := range all {
		var e entry
		if json.Unmarshal(rawEntry, &e) != nil || e.Input == nil || e.Output == nil {
			continue
		}
		book[name] = toPrice(e)
	}
	if len(book) == 0 {
		return nil, errors.New("litellm: price list has no usable entries")
	}
	return book, nil
}

func toPrice(e entry) cost.Price {
	p := cost.Price{Input: *e.Input, Output: *e.Output, CacheRead: *e.Input, CacheWrite5m: *e.Input}
	if e.CacheRead != nil {
		p.CacheRead = *e.CacheRead
	}
	if e.CacheWrite != nil {
		p.CacheWrite5m = *e.CacheWrite
	}
	p.CacheWrite1h = p.CacheWrite5m
	if e.CacheWriteAbove != nil {
		p.CacheWrite1h = *e.CacheWriteAbove
	}
	return p
}

func (b Book) Lookup(model string) (cost.Price, bool) {
	if i := strings.IndexByte(model, '['); i > 0 {
		model = model[:i]
	}
	for _, key := range []string{model, "anthropic/" + model, "openai/" + model} {
		if p, ok := b[key]; ok {
			return p, true
		}
	}
	return cost.Price{}, false
}

type Store struct {
	Path string
}

func (s Store) Load() (Book, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotInstalled
	}
	if err != nil {
		return nil, fmt.Errorf("litellm: read %s: %w", s.Path, err)
	}
	return Parse(raw)
}

func (s Store) Update(ctx context.Context, client *http.Client, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("litellm: download prices: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("litellm: download prices: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return 0, fmt.Errorf("litellm: download prices: %w", err)
	}
	book, err := Parse(raw)
	if err != nil {
		return 0, err
	}
	if err := writeAtomic(s.Path, raw); err != nil {
		return 0, err
	}
	return len(book), nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".prices-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
