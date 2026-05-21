package main

import (
	"testing"
)

func TestAllProviders_Count(t *testing.T) {
	providers := allProviders()
	if len(providers) != 9 {
		t.Fatalf("allProviders() returned %d providers, want 9", len(providers))
	}
}

func TestAllProviders_ChannelNames(t *testing.T) {
	want := map[string]bool{
		"mcpServers":         true,
		"hooks":              true,
		"commands":           true,
		"rules":              true,
		"skills":             true,
		"plugins":            true,
		"cliTools":           true,
		"backgroundServices": true,
		"tools":              true,
	}

	providers := allProviders()
	got := make(map[string]bool, len(providers))
	for _, p := range providers {
		ch := p.Channel()
		if got[ch] {
			t.Errorf("duplicate channel name %q", ch)
		}
		got[ch] = true
	}

	for ch := range want {
		if !got[ch] {
			t.Errorf("missing channel %q", ch)
		}
	}
	for ch := range got {
		if !want[ch] {
			t.Errorf("unexpected channel %q", ch)
		}
	}
}

func TestBuildEnv_Fields(t *testing.T) {
	dir := t.TempDir()
	env := buildEnv(dir)

	if env.Root != dir {
		t.Errorf("Root = %q, want %q", env.Root, dir)
	}
	if env.FS == nil {
		t.Error("FS is nil, want non-nil")
	}
	if env.Runner == nil {
		t.Error("Runner is nil, want non-nil")
	}
	if env.Fetch == nil {
		t.Error("Fetch is nil, want non-nil")
	}
}
