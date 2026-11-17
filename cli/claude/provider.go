package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/sifatulrabbi/sifatul-stack/cli/usage"
)

const (
	DefaultBaseURL = "https://api.anthropic.com"
	refreshHint    = "open Claude Code once to refresh it"
)

type Provider struct {
	Credentials CredentialStore
	HTTP        *http.Client
	BaseURL     string
	Now         func() time.Time
}

func (p Provider) Name() string { return "claude" }

func (p Provider) Usage(ctx context.Context) (usage.Report, error) {
	creds, err := p.Credentials.Load(ctx)
	if err != nil {
		return usage.Report{}, err
	}
	if !creds.ExpiresAt.IsZero() && p.Now().After(creds.ExpiresAt) {
		return usage.Report{}, fmt.Errorf("claude: access token expired at %s (%s)", creds.ExpiresAt.Format(time.RFC3339), refreshHint)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/api/oauth/usage", nil)
	if err != nil {
		return usage.Report{}, err
	}
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	// Undocumented endpoint used by Claude Code's /usage; needs this beta header: https://docs.rs/crate/cship/1.7.1/source/src/usage_limits.rs
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Accept", "application/json")

	resp, err := p.HTTP.Do(req)
	if err != nil {
		return usage.Report{}, fmt.Errorf("claude: request usage: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Println("[[WARN]] Failed to close the request body:", err)
		}
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return usage.Report{}, fmt.Errorf("claude: read usage: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return usage.Report{}, fmt.Errorf("claude: usage endpoint returned HTTP %d (%s)", resp.StatusCode, refreshHint)
	}
	if resp.StatusCode != http.StatusOK {
		return usage.Report{}, fmt.Errorf("claude: usage endpoint returned HTTP %d", resp.StatusCode)
	}

	windows, err := ParseUsage(body)
	if err != nil {
		return usage.Report{}, err
	}
	return usage.Report{Provider: p.Name(), Plan: planLabel(creds), Windows: windows}, nil
}

func planLabel(c Credentials) string {
	if c.Tier == "" {
		return c.Plan
	}
	if c.Plan == "" {
		return c.Tier
	}
	return c.Plan + " (" + c.Tier + ")"
}

var groupMinutes = map[string]int{
	"session": 5 * 60,
	"weekly":  7 * 24 * 60,
}

var legacyWindows = map[string]struct {
	order   int
	minutes int
}{
	"five_hour":        {0, 5 * 60},
	"seven_day":        {1, 7 * 24 * 60},
	"seven_day_opus":   {2, 7 * 24 * 60},
	"seven_day_sonnet": {3, 7 * 24 * 60},
}

type limit struct {
	Kind     string  `json:"kind"`
	Group    string  `json:"group"`
	Percent  float64 `json:"percent"`
	ResetsAt *string `json:"resets_at"`
	Scope    *struct {
		Model *struct {
			DisplayName string `json:"display_name"`
		} `json:"model"`
	} `json:"scope"`
}

type usageResponse struct {
	Limits     []limit `json:"limits"`
	ExtraUsage *struct {
		IsEnabled     bool     `json:"is_enabled"`
		Utilization   *float64 `json:"utilization"`
		MonthlyLimit  *float64 `json:"monthly_limit"`
		UsedCredits   *float64 `json:"used_credits"`
		Currency      string   `json:"currency"`
		DecimalPlaces *int     `json:"decimal_places"`
	} `json:"extra_usage"`
}

type legacyWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

func ParseUsage(body []byte) ([]usage.Window, error) {
	var resp usageResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("claude: parse usage: %w", err)
	}

	var windows []usage.Window
	var err error
	if len(resp.Limits) > 0 {
		windows, err = fromLimits(resp.Limits)
	} else {
		windows, err = fromLegacyFields(body)
	}
	if err != nil {
		return nil, err
	}

	if x := resp.ExtraUsage; x != nil && x.IsEnabled && x.Utilization != nil {
		w := usage.Window{Name: "extra_usage", UsedPercent: *x.Utilization, Spend: true}
		if x.MonthlyLimit != nil && x.UsedCredits != nil && x.DecimalPlaces != nil && x.Currency != "" {
			scale := math.Pow10(*x.DecimalPlaces)
			w.Budget = &usage.Budget{Spent: *x.UsedCredits / scale, Limit: *x.MonthlyLimit / scale, Currency: x.Currency}
		}
		windows = append(windows, w)
	}
	return windows, nil
}

func fromLimits(limits []limit) ([]usage.Window, error) {
	windows := make([]usage.Window, 0, len(limits))
	for _, l := range limits {
		name := l.Kind
		if l.Scope != nil && l.Scope.Model != nil && l.Scope.Model.DisplayName != "" {
			name += ":" + l.Scope.Model.DisplayName
		}
		resetsAt, err := parseTime(name, l.ResetsAt)
		if err != nil {
			return nil, err
		}
		windows = append(windows, usage.Window{Name: name, UsedPercent: l.Percent, ResetsAt: resetsAt, WindowMinutes: groupMinutes[l.Group]})
	}
	return windows, nil
}

func fromLegacyFields(body []byte) ([]usage.Window, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, fmt.Errorf("claude: parse usage: %w", err)
	}

	var windows []usage.Window
	for name, raw := range fields {
		if name == "extra_usage" {
			continue
		}
		var w legacyWindow
		if json.Unmarshal(raw, &w) != nil || w.Utilization == nil {
			continue
		}
		resetsAt, err := parseTime(name, w.ResetsAt)
		if err != nil {
			return nil, err
		}
		windows = append(windows, usage.Window{Name: name, UsedPercent: *w.Utilization, ResetsAt: resetsAt, WindowMinutes: legacyWindows[name].minutes})
	}

	sort.Slice(windows, func(i, j int) bool {
		oi, iKnown := legacyWindows[windows[i].Name]
		oj, jKnown := legacyWindows[windows[j].Name]
		if iKnown != jKnown {
			return iKnown
		}
		if iKnown && oi.order != oj.order {
			return oi.order < oj.order
		}
		return windows[i].Name < windows[j].Name
	})
	return windows, nil
}

func parseTime(field string, s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		return nil, fmt.Errorf("claude: parse %s.resets_at: %w", field, err)
	}
	return &t, nil
}
