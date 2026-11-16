package main

import (
	"errors"
	"testing"
)

func TestZoneNameFromTZOrTheLocaltimeLink(t *testing.T) {
	noLink := func(string) (string, error) { return "", errors.New("no link") }
	for _, tt := range []struct {
		tz, link, want string
	}{
		{"Europe/Berlin", "/var/db/timezone/zoneinfo/Asia/Dhaka", "Europe/Berlin"},
		{":Europe/Berlin", "", "Europe/Berlin"},
		{"", "/var/db/timezone/zoneinfo/Asia/Dhaka", "Asia/Dhaka"},
		{"", "../usr/share/zoneinfo/America/New_York", "America/New_York"},
		{"", "", ""},
	} {
		readlink := noLink
		if tt.link != "" {
			readlink = func(string) (string, error) { return tt.link, nil }
		}
		if got := zoneName(func(string) string { return tt.tz }, readlink); got != tt.want {
			t.Errorf("tz=%q link=%q: got %q, want %q", tt.tz, tt.link, got, tt.want)
		}
	}
}

func TestBarWidthFitsTheTerminal(t *testing.T) {
	for cols, want := range map[int]int{0: 50, 200: 50, 60: 46, 20: 10} {
		if got := barWidth(cols); got != want {
			t.Errorf("barWidth(%d) = %d, want %d", cols, got, want)
		}
	}
}
