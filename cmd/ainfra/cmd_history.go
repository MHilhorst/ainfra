package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/MHilhorst/ainfra/internal/cli"
	"github.com/MHilhorst/ainfra/internal/provider"
	"github.com/MHilhorst/ainfra/internal/ui"
)

// newHistoryCommand reads the apply-history log and prints filtered events.
// The log is the per-repo .ainfra/history.jsonl appended after every apply.
func newHistoryCommand() *cli.Command {
	var since, actor, channel string
	var asJSON bool
	return &cli.Command{
		Name:      "history",
		Summary:   "Show recent apply events (who/what/when)",
		UsageLine: "ainfra history [--since 7d] [--actor <email>] [--channel <name>] [--json]",
		Example:   "ainfra history --since 24h --channel mcpServers",
		Hidden:    true,
		SetFlags: func(fs *flag.FlagSet) {
			fs.StringVar(&since, "since", "7d", "only events newer than this (e.g. 24h, 7d, 2026-05-20)")
			fs.StringVar(&actor, "actor", "", "only events by this actor (git user.email)")
			fs.StringVar(&channel, "channel", "", "only events in this channel (e.g. mcpServers)")
			fs.BoolVar(&asJSON, "json", false, "print as JSON Lines instead of a table")
		},
		Run: func(ctx cli.Context) int {
			return runHistory(ctx, since, actor, channel, asJSON)
		},
	}
}

func runHistory(ctx cli.Context, since, actor, channel string, asJSON bool) int {
	errColor := ui.NewColorizer(ctx.Stderr, ctx.NoColor)
	events, err := provider.ReadHistory(ctx.Dir)
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}
	cutoff, err := parseSince(since, time.Now().UTC())
	if err != nil {
		ui.RenderError(ctx.Stderr, errColor, err)
		return 1
	}

	filtered := make([]provider.HistoryEvent, 0, len(events))
	for _, e := range events {
		if actor != "" && e.Actor != actor {
			continue
		}
		if channel != "" && e.Channel != channel {
			continue
		}
		if !cutoff.IsZero() {
			ts, terr := time.Parse(time.RFC3339Nano, e.TS)
			if terr != nil {
				// Tolerate malformed timestamps — include them rather than
				// drop them silently. A bad ts is its own diagnostic.
				filtered = append(filtered, e)
				continue
			}
			if ts.Before(cutoff) {
				continue
			}
		}
		filtered = append(filtered, e)
	}

	if asJSON {
		enc := json.NewEncoder(ctx.Stdout)
		for _, e := range filtered {
			_ = enc.Encode(&e)
		}
		return 0
	}

	if len(filtered) == 0 {
		fmt.Fprintln(ctx.Stdout, "No history.")
		return 0
	}
	for _, e := range filtered {
		fmt.Fprintf(ctx.Stdout, "%s  %-32s  %-12s  %-14s %s/%s\n",
			e.TS, truncate(e.Actor, 32), e.Command, e.Kind, e.Channel, e.ID)
	}
	return 0
}

// parseSince accepts a relative duration (e.g. "24h", "7d") or an RFC3339 or
// YYYY-MM-DD date. The zero time means "no lower bound".
func parseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if strings.HasSuffix(s, "d") {
		days, err := time.ParseDuration(strings.TrimSuffix(s, "d") + "h")
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid --since %q", s)
		}
		return now.Add(-days * 24), nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q (use 24h, 7d, or YYYY-MM-DD)", s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 1 {
		return ""
	}
	return s[:n-1] + "…"
}
