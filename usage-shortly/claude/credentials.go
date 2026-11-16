package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type Credentials struct {
	AccessToken string
	ExpiresAt   time.Time
	Plan        string
	Tier        string
}

type CredentialStore interface {
	Load(ctx context.Context) (Credentials, error)
}

type credentialsFile struct {
	ClaudeAiOauth *struct {
		AccessToken      string `json:"accessToken"`
		ExpiresAt        int64  `json:"expiresAt"`
		SubscriptionType string `json:"subscriptionType"`
		RateLimitTier    string `json:"rateLimitTier"`
	} `json:"claudeAiOauth"`
}

var ErrNotLoggedIn = errors.New("claude: no claude.ai OAuth login found (run `claude` and /login)")

func ParseCredentials(raw []byte) (Credentials, error) {
	var f credentialsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return Credentials{}, fmt.Errorf("claude: parse credentials: %w", err)
	}
	if f.ClaudeAiOauth == nil || f.ClaudeAiOauth.AccessToken == "" {
		return Credentials{}, ErrNotLoggedIn
	}
	return Credentials{
		AccessToken: f.ClaudeAiOauth.AccessToken,
		ExpiresAt:   time.UnixMilli(f.ClaudeAiOauth.ExpiresAt),
		Plan:        f.ClaudeAiOauth.SubscriptionType,
		Tier:        f.ClaudeAiOauth.RateLimitTier,
	}, nil
}

type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

type KeychainStore struct {
	Run CommandRunner
}

func (k KeychainStore) Load(ctx context.Context) (Credentials, error) {
	out, err := k.Run(ctx, "security", "find-generic-password", "-s", "Claude Code-credentials", "-w")
	if err != nil {
		return Credentials{}, ErrNotLoggedIn
	}
	return ParseCredentials(out)
}

type FileStore struct {
	Path string
}

func (f FileStore) Load(context.Context) (Credentials, error) {
	raw, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Credentials{}, ErrNotLoggedIn
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("claude: read %s: %w", f.Path, err)
	}
	return ParseCredentials(raw)
}

type FirstAvailable []CredentialStore

func (stores FirstAvailable) Load(ctx context.Context) (Credentials, error) {
	err := ErrNotLoggedIn
	for _, s := range stores {
		c, e := s.Load(ctx)
		if e == nil {
			return c, nil
		}
		if !errors.Is(e, ErrNotLoggedIn) {
			err = e
		}
	}
	return Credentials{}, err
}
