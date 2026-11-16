package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/usage-shortly/claude"
	"github.com/sifatulrabbi/sifatul-stack/usage-shortly/codex"
	"github.com/sifatulrabbi/sifatul-stack/usage-shortly/cost"
	"github.com/sifatulrabbi/sifatul-stack/usage-shortly/litellm"
	"github.com/sifatulrabbi/sifatul-stack/usage-shortly/usage"
)

func main() {
	err := newRootCmd(defaultApp()).ExecuteContext(context.Background())
	if err != nil && !errors.Is(err, errProviderFailed) {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}
	os.Exit(exitCode(err))
}

var priceAliases = map[string]string{
	"codex-auto-review": "gpt-6.1-terra",
}

func defaultApp() app {
	home, _ := os.UserHomeDir()
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = filepath.Join(home, ".cache")
	}
	store := litellm.Store{Path: filepath.Join(cacheDir, "usage-shortly", "litellm_prices.json")}
	codexDir := envOr("CODEX_HOME", filepath.Join(home, ".codex"))
	claudeDir := envOr("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))

	return app{
		providers: defaultProviders(home, codexDir),
		now:       time.Now,
		weekly: weeklyCost{
			Logs: map[string]cost.Log{
				"claude": claude.TranscriptLog{FS: os.DirFS(filepath.Join(claudeDir, "projects"))},
				"codex":  codex.UsageLog{FS: os.DirFS(filepath.Join(codexDir, "sessions"))},
			},
			Prices: func() (cost.PriceBook, error) {
				book, err := store.Load()
				if err != nil {
					return nil, err
				}
				return cost.WithAliases(book, priceAliases), nil
			},
		},
		updatePrices: func(ctx context.Context) (int, error) {
			return store.Update(ctx, &http.Client{Timeout: 60 * time.Second}, litellm.DefaultURL)
		},
		pricesPath: store.Path,
	}
}

func defaultProviders(home, codexDir string) []usage.Provider {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	return []usage.Provider{
		claude.Provider{
			Credentials: claude.FirstAvailable{
				claude.KeychainStore{Run: claude.ExecRunner},
				claude.FileStore{Path: filepath.Join(home, ".claude", ".credentials.json")},
			},
			HTTP:    httpClient,
			BaseURL: claude.DefaultBaseURL,
			Now:     time.Now,
		},
		codex.Provider{
			Live:     codex.AppServerSource{Start: codex.ExecAppServer(envOr("USAGE_CODEX_BIN", "codex")), Now: time.Now},
			Fallback: codex.RolloutSource{FS: os.DirFS(filepath.Join(codexDir, "sessions"))},
		},
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
