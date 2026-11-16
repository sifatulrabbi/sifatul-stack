package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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
	if !strings.Contains(out, "claude") || !strings.Contains(out, "codex") || !strings.Contains(out, "75% left") {
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
	if !strings.Contains(out, "error: token expired") || !strings.Contains(out, "75% left") {
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
