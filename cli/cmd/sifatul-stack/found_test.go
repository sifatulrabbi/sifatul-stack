package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sifatulrabbi/sifatul-stack/cli/stack"
)

func TestHarnessFoundByConfigDirOrBinary(t *testing.T) {
	home := t.TempDir()
	layout := stack.LayoutFromEnv(home, func(string) string { return "" })
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	lookPath := func(bin string) (string, error) {
		if bin == "opencode" {
			return "/usr/local/bin/opencode", nil
		}
		return "", errors.New("not found")
	}
	found := harnessFound(layout, lookPath)

	for key, want := range map[string]bool{"claude": true, "codex": false, "opencode": true} {
		h, err := stack.Find(key)
		if err != nil {
			t.Fatal(err)
		}
		if got := found(h); got != want {
			t.Errorf("found(%s) = %v, want %v", key, got, want)
		}
	}
}
