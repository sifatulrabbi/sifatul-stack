package claude

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const sampleUsage = `{
  "five_hour": {"utilization": 37.0, "resets_at": "2026-11-16T14:00:00.123456+00:00"},
  "seven_day": {"utilization": 12.5, "resets_at": "2026-11-20T09:00:00+00:00"},
  "seven_day_opus": null,
  "seven_day_sonnet": {"utilization": 4, "resets_at": null},
  "seven_day_oauth_apps": {"utilization": 1, "resets_at": null},
  "extra_usage": {"is_enabled": false, "monthly_limit": null}
}`

type staticStore struct {
	creds Credentials
	err   error
}

func (s staticStore) Load(context.Context) (Credentials, error) { return s.creds, s.err }

var now = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

func newProvider(t *testing.T, handler http.HandlerFunc, creds Credentials) Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return Provider{
		Credentials: staticStore{creds: creds},
		HTTP:        srv.Client(),
		BaseURL:     srv.URL,
		Now:         func() time.Time { return now },
	}
}

func validCreds() Credentials {
	return Credentials{AccessToken: "tok", ExpiresAt: now.Add(time.Hour), Plan: "max", Tier: "default_claude_max_5x"}
}

func TestUsageSendsOAuthHeadersAndParsesWindows(t *testing.T) {
	p := newProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/usage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
			t.Errorf("anthropic-beta = %q", got)
		}
		w.Write([]byte(sampleUsage))
	}, validCreds())

	r, err := p.Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Plan != "max (default_claude_max_5x)" {
		t.Errorf("plan = %q", r.Plan)
	}
	var names []string
	for _, w := range r.Windows {
		names = append(names, w.Name)
	}
	if got := strings.Join(names, ","); got != "five_hour,seven_day,seven_day_sonnet,seven_day_oauth_apps" {
		t.Fatalf("windows = %s", got)
	}
	five := r.Windows[0]
	if five.UsedPercent != 37 || five.WindowMinutes != 300 || five.ResetsAt.Hour() != 14 {
		t.Errorf("five_hour = %+v", five)
	}
	if r.Windows[2].ResetsAt != nil {
		t.Errorf("null resets_at should stay nil")
	}
}

func TestExpiredTokenFailsWithoutCallingAPI(t *testing.T) {
	creds := validCreds()
	creds.ExpiresAt = now.Add(-time.Minute)
	p := newProvider(t, func(http.ResponseWriter, *http.Request) {
		t.Error("API must not be called with an expired token")
	}, creds)

	_, err := p.Usage(context.Background())
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v", err)
	}
}

func TestNonOKStatusIsAnError(t *testing.T) {
	p := newProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}, validCreds())

	_, err := p.Usage(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseCredentialsReadsClaudeAiOauthOnly(t *testing.T) {
	c, err := ParseCredentials([]byte(`{"mcpOAuth":{"x":{"accessToken":"nope"}},"claudeAiOauth":{"accessToken":"a","expiresAt":1763290800000,"subscriptionType":"team","rateLimitTier":"t"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.AccessToken != "a" || c.Plan != "team" || c.ExpiresAt.UnixMilli() != 1763290800000 {
		t.Fatalf("creds = %+v", c)
	}

	_, err = ParseCredentials([]byte(`{"mcpOAuth":{}}`))
	if !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("err = %v, want ErrNotLoggedIn", err)
	}
}

func TestFirstAvailableFallsThroughNotLoggedIn(t *testing.T) {
	stores := FirstAvailable{
		staticStore{err: ErrNotLoggedIn},
		staticStore{creds: Credentials{AccessToken: "file"}},
	}
	c, err := stores.Load(context.Background())
	if err != nil || c.AccessToken != "file" {
		t.Fatalf("creds = %+v, err = %v", c, err)
	}
}

func TestKeychainStoreQueriesClaudeCodeService(t *testing.T) {
	var gotArgs []string
	k := KeychainStore{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = append([]string{name}, args...)
		return []byte(`{"claudeAiOauth":{"accessToken":"k"}}`), nil
	}}
	c, err := k.Load(context.Background())
	if err != nil || c.AccessToken != "k" {
		t.Fatalf("creds = %+v, err = %v", c, err)
	}
	if got := strings.Join(gotArgs, " "); got != "security find-generic-password -s Claude Code-credentials -w" {
		t.Fatalf("command = %s", got)
	}
}

func TestParseLivePayloadPrefersLimitsArrayAndAddsExtraUsage(t *testing.T) {
	body, err := os.ReadFile("testdata/usage_2026-11-16.json")
	if err != nil {
		t.Fatal(err)
	}
	windows, err := ParseUsage(body)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, w := range windows {
		got = append(got, fmt.Sprintf("%s=%g/%d", w.Name, w.UsedPercent, w.WindowMinutes))
	}
	want := "session=1/300,weekly_all=0/10080,weekly_scoped:Fable=0/10080,extra_usage=25.549999999999997/0"
	if strings.Join(got, ",") != want {
		t.Fatalf("windows = %s\nwant      %s", strings.Join(got, ","), want)
	}
	if windows[0].ResetsAt == nil || windows[0].ResetsAt.Hour() != 8 || windows[3].ResetsAt != nil {
		t.Fatalf("resets_at not mapped: %+v", windows)
	}
	if windows[0].Spend || !windows[3].Spend {
		t.Fatalf("resets_at not mapped: %+v", windows)
	}
}

func TestDisabledExtraUsageIsOmitted(t *testing.T) {
	windows, err := ParseUsage([]byte(`{"limits":[{"kind":"session","group":"session","percent":5}],"extra_usage":{"is_enabled":false,"utilization":0}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 1 || windows[0].Name != "session" {
		t.Fatalf("windows = %+v", windows)
	}
}
