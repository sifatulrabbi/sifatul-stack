package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/sifatulrabbi/sifatul-stack/cli/claude"
	"github.com/sifatulrabbi/sifatul-stack/cli/codex"
	"github.com/sifatulrabbi/sifatul-stack/cli/render"
	"github.com/sifatulrabbi/sifatul-stack/cli/source"
	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
	"github.com/sifatulrabbi/sifatul-stack/cli/usage"
)

func main() {
	err := newRootCmd(defaultApp()).ExecuteContext(context.Background())
	if err != nil && !errors.Is(err, errProviderFailed) {
		ui.Print(os.Stderr, ui.Bad.Render("✗ error:")+" "+err.Error()+"\n")
	}
	os.Exit(exitCode(err))
}

func defaultApp() app {
	home, _ := os.UserHomeDir()
	codexDir := envOr("CODEX_HOME", filepath.Join(home, ".codex"))
	animate := term.IsTerminal(os.Stdout.Fd()) && os.Getenv("TERM") != "dumb"
	cols, _, _ := term.GetSize(os.Stdout.Fd())
	layout := stack.LayoutFromEnv(home, os.Getenv)
	var choose func(context.Context, []ui.Choice) ([]string, error)
	if animate && term.IsTerminal(os.Stdin.Fd()) {
		choose = func(ctx context.Context, choices []ui.Choice) ([]string, error) {
			return ui.Choose(ctx, os.Stdin, os.Stdout, "Install sifatul-stack into which harnesses?", choices)
		}
	}

	return app{
		providers: defaultProviders(home, codexDir),
		now:       time.Now,
		animate:   animate,
		text: render.TextView{
			Width: barWidth(cols),
			Loc:   localZone(),
			Color: animate && os.Getenv("NO_COLOR") == "",
		},
		stack: stackEnv{
			layout:  layout,
			dataDir: filepath.Join(envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share")), "sifatul-stack"),
			repoURL: envOr("SIFATUL_STACK_REPO", source.DefaultURL),
			git:     source.ExecGit,
			now:     time.Now,
			animate: animate,
			home:    home,
			choose:  choose,
			found:   harnessFound(layout, exec.LookPath),
		},
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

func harnessFound(layout stack.Layout, lookPath func(string) (string, error)) func(stack.Harness) bool {
	return func(h stack.Harness) bool {
		if _, err := os.Stat(h.Dir(layout)); err == nil {
			return true
		}
		_, err := lookPath(h.Binary)
		return err == nil
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func zoneName(getenv func(string) string, readlink func(string) (string, error)) string {
	if tz := strings.TrimPrefix(getenv("TZ"), ":"); tz != "" {
		return tz
	}
	target, err := readlink("/etc/localtime")
	if err != nil {
		return ""
	}
	_, name, _ := strings.Cut(target, "zoneinfo/")
	return name
}

func localZone() *time.Location {
	name := zoneName(os.Getenv, os.Readlink)
	if loc, err := time.LoadLocation(name); err == nil && name != "" {
		return loc
	}
	return time.Local
}

func barWidth(cols int) int {
	const indent, figure = 2, 12
	if cols <= 0 {
		return 50
	}
	return max(10, min(50, cols-indent-figure))
}
