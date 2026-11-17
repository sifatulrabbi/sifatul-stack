package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/codex"
	"github.com/sifatulrabbi/sifatul-stack/cli/ui"
	"github.com/sifatulrabbi/sifatul-stack/cli/usage"
)

type fakeProvider struct {
	name string
	err  error
	hits *int
}

func (f fakeProvider) Name() string { return f.name }

func (f fakeProvider) Usage(context.Context) (usage.Report, error) {
	if f.hits != nil {
		*f.hits++
	}
	if f.err != nil {
		return usage.Report{}, f.err
	}
	return usage.Report{Provider: f.name, Windows: []usage.Window{{Name: "weekly", UsedPercent: 25}}}, nil
}

var fixedNow = func() time.Time { return time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC) }

func execute(t *testing.T, providers []usage.Provider, args ...string) (string, error) {
	t.Helper()
	cmd := newUsageCmd(app{providers: providers, now: fixedNow})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestDefaultRunsAllProvidersAsText(t *testing.T) {
	out, err := execute(t, []usage.Provider{fakeProvider{name: "claude"}, fakeProvider{name: "codex"}})
	if exitCode(err) != 0 {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(out, "Claude") || !strings.Contains(out, "Codex") || !strings.Contains(out, "25% used") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestPositionalArgSelectsProvider(t *testing.T) {
	claudeHits, codexHits := 0, 0
	_, err := execute(t, []usage.Provider{fakeProvider{name: "claude", hits: &claudeHits}, fakeProvider{name: "codex", hits: &codexHits}}, "codex")
	if err != nil || claudeHits != 0 || codexHits != 1 {
		t.Fatalf("err = %v, claude = %d, codex = %d", err, claudeHits, codexHits)
	}
}

func TestJSONFlagEmitsParsableJSON(t *testing.T) {
	out, err := execute(t, []usage.Provider{fakeProvider{name: "codex"}}, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Reports []usage.Report `json:"reports"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Reports[0].Provider != "codex" {
		t.Fatalf("output %q, err %v", out, err)
	}
}

func TestFailingProviderStillPrintsAndExitsOne(t *testing.T) {
	out, err := execute(t, []usage.Provider{fakeProvider{name: "claude", err: errors.New("token expired")}, fakeProvider{name: "codex"}})
	if exitCode(err) != 1 {
		t.Fatalf("exit = %d, err = %v", exitCode(err), err)
	}
	if !strings.Contains(out, "error: token expired") || !strings.Contains(out, "25% used") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestUnknownProviderExitsTwo(t *testing.T) {
	_, err := execute(t, []usage.Provider{fakeProvider{name: "codex"}}, "gemini")
	if exitCode(err) != 2 || !strings.Contains(err.Error(), "gemini") {
		t.Fatalf("exit = %d, err = %v", exitCode(err), err)
	}
}

func TestBadFlagExitsTwo(t *testing.T) {
	_, err := execute(t, []usage.Provider{fakeProvider{name: "codex"}}, "--nope")
	if exitCode(err) != 2 {
		t.Fatalf("exit = %d, err = %v", exitCode(err), err)
	}
}

func TestJSONIsSingleLineByDefault(t *testing.T) {
	out, err := execute(t, []usage.Provider{fakeProvider{name: "codex"}}, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, `"left":75`) {
		t.Fatalf("output %q", out)
	}
}

func TestPrettyJSONIsIndentedFullReport(t *testing.T) {
	out, err := execute(t, []usage.Provider{fakeProvider{name: "codex"}}, "--json", "--pretty")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "\n") < 5 || !strings.Contains(out, `"used_percent": 25`) {
		t.Fatalf("output %q", out)
	}
}

func TestPrettyWithoutJSONExitsTwo(t *testing.T) {
	_, err := execute(t, []usage.Provider{fakeProvider{name: "codex"}}, "--pretty")
	if exitCode(err) != 2 || !strings.Contains(err.Error(), "--pretty requires --json") {
		t.Fatalf("exit = %d, err = %v", exitCode(err), err)
	}
}

func TestFinishStripsTerminalEscapesFromErrors(t *testing.T) {
	t.Setenv("TTY_FORCE", "1")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")
	var buf bytes.Buffer
	code := finish(&buf, errors.New("git pull: remote: \x1b]52;c;ZWNobyBwd25lZA==\x07hi"))
	if code != 2 || strings.Contains(buf.String(), "\x1b]") || strings.Contains(buf.String(), "\x07") || !strings.Contains(buf.String(), "remote: hi") {
		t.Fatalf("code = %d, out = %q", code, buf.String())
	}
}

func TestFinishStaysQuietWhenAProviderFailed(t *testing.T) {
	var buf bytes.Buffer
	if code := finish(&buf, errProviderFailed); code != 1 || buf.Len() != 0 {
		t.Fatalf("code = %d, out = %q", code, buf.String())
	}
}

type blockingProvider struct{ started chan struct{} }

func (blockingProvider) Name() string { return "claude" }

func (b blockingProvider) Usage(ctx context.Context) (usage.Report, error) {
	close(b.started)
	<-ctx.Done()
	return usage.Report{}, ctx.Err()
}

func TestInterruptedUsagePrintsNothingAndExits130(t *testing.T) {
	for _, args := range [][]string{nil, {"--json"}} {
		p := blockingProvider{started: make(chan struct{})}
		ctx, interrupt := context.WithCancelCause(context.Background())
		go func() {
			<-p.started
			interrupt(ui.ErrInterrupted)
		}()
		cmd := newUsageCmd(app{providers: []usage.Provider{p}, now: fixedNow})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		code := finish(&out, cmd.ExecuteContext(ctx))
		if code != 130 || out.Len() != 0 {
			t.Fatalf("%v: code = %d, out = %q", args, code, out.String())
		}
	}
}

func TestFirstInterruptCancelsAndSecondExits130(t *testing.T) {
	exited := make(chan int, 1)
	ctx, stop := interruptible(context.Background(), func(code int) { exited <- code })
	defer stop()
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("first SIGINT did not cancel")
	}
	if !errors.Is(ui.Interrupted(ctx), ui.ErrInterrupted) {
		t.Fatalf("cause = %v", context.Cause(ctx))
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-exited:
		if code != 130 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second SIGINT did not exit")
	}
}

func TestFinishTreatsAnInterruptAsQuiet130(t *testing.T) {
	var buf bytes.Buffer
	if code := finish(&buf, failed(fmt.Errorf("git clone: %w", ui.ErrInterrupted))); code != 130 || buf.Len() != 0 {
		t.Fatalf("code = %d, out = %q", code, buf.String())
	}
}

type hangingSnapshots struct{ entered chan struct{} }

func (h hangingSnapshots) Read(ctx context.Context) (codex.Reading, error) {
	if h.entered != nil {
		close(h.entered)
	}
	<-ctx.Done()
	return codex.Reading{}, ctx.Err()
}

func TestInterruptDuringTheCodexFallbackStopsUsage(t *testing.T) {
	for _, args := range [][]string{{"--json"}, nil} {
		fallback := hangingSnapshots{entered: make(chan struct{})}
		p := codex.Provider{Live: hangingSnapshots{}, Fallback: fallback}
		ctx, interrupt := context.WithCancelCause(context.Background())
		go func() {
			<-fallback.entered
			interrupt(ui.ErrInterrupted)
		}()
		cmd := newUsageCmd(app{providers: []usage.Provider{p}, now: fixedNow})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(append(args, "--timeout", "50ms"))

		start := time.Now()
		code := finish(&out, cmd.ExecuteContext(ctx))

		if took := time.Since(start); code != 130 || out.Len() != 0 || took > time.Second {
			t.Fatalf("%v: code = %d after %s, out = %q", args, code, took, out.String())
		}
	}
}
