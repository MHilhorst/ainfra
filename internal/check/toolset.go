// Package check implements ainfra check's runtime validations against the
// lockfile — toolset drift, secret resolvability, precondition gates. The
// toolset-drift checker re-introspects every MCP server with a populated
// ToolsetHash and reports a per-tool diff when the live tool list no longer
// matches the locked fingerprint.
package check

import (
	"context"
	"sort"

	"github.com/MHilhorst/ainfra/internal/lockfile"
	"github.com/MHilhorst/ainfra/internal/mcpclient"
)

// IntrospectRunner overrides the mcpclient.Runner used when re-introspecting
// MCP servers at check time. Nil means use mcpclient.DefaultRunner. Tests
// inject a FakeRunner; the resolve package's DisableIntrospection sentinel
// works here too — entries hit by it are reported as introspection failures
// (distinct from drift).
var IntrospectRunner mcpclient.Runner

// ToolDiffKind classifies what changed about one tool between the locked and
// live toolsets.
type ToolDiffKind int

const (
	// ToolAdded means the live toolset has a tool the locked entry didn't.
	ToolAdded ToolDiffKind = iota
	// ToolRemoved means the locked entry had a tool the live toolset doesn't.
	ToolRemoved
	// ToolDescriptionChanged means the tool's description hash differs.
	ToolDescriptionChanged
	// ToolInputSchemaChanged means the tool's input schema hash differs.
	ToolInputSchemaChanged
)

func (k ToolDiffKind) String() string {
	switch k {
	case ToolAdded:
		return "added"
	case ToolRemoved:
		return "removed"
	case ToolDescriptionChanged:
		return "description changed"
	case ToolInputSchemaChanged:
		return "input schema changed"
	default:
		return "unknown"
	}
}

// ToolDiff is one per-tool change identified during a drift comparison.
type ToolDiff struct {
	Name string
	Kind ToolDiffKind
}

// ToolsetDrift is the report for one MCP server whose live toolset no longer
// matches its locked ToolsetHash, or which could not be re-introspected at
// check time.
type ToolsetDrift struct {
	ServerID   string
	LockedHash string
	LiveHash   string
	Diff       []ToolDiff
	// IntrospectErr, when non-empty, indicates the server could not be
	// introspected at check time (subprocess failure, timeout, protocol error).
	// LockedHash, LiveHash and Diff are not meaningful when IntrospectErr is
	// set — render the error instead.
	IntrospectErr string
}

// Report aggregates the result of a CheckToolsetDrift run.
type Report struct {
	Drifts []ToolsetDrift
	// UnverifiedCount is the number of MCP entries skipped because their
	// ToolsetHash was empty at lock time. Surface this as an informational
	// note; it never contributes to drift.
	UnverifiedCount int
}

// HasDrift reports whether any mismatch or introspection failure was found.
// Callers use this to decide the exit code of `ainfra check`.
func (r Report) HasDrift() bool { return len(r.Drifts) > 0 }

// CheckToolsetDrift re-introspects every MCP server in committed and personal
// that has a populated ToolsetHash, compares against the locked fingerprint,
// and returns a Report. Entries without a stored ToolsetHash are counted as
// unverified and contribute to Report.UnverifiedCount only.
func CheckToolsetDrift(committed, personal *lockfile.Lock) Report {
	return checkToolsetDriftWithRunner(committed, personal, IntrospectRunner)
}

func checkToolsetDriftWithRunner(committed, personal *lockfile.Lock, runner mcpclient.Runner) Report {
	entries := mergeMCPEntries(committed, personal)

	report := Report{}
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		entry := entries[id]
		if entry.ToolsetHash == "" {
			report.UnverifiedCount++
			continue
		}
		if entry.Command == "" {
			// Locked with a hash but no command recorded — older lockfile
			// schema. Surface as an introspection failure so the user is
			// nudged to re-lock; treat as drift.
			report.Drifts = append(report.Drifts, ToolsetDrift{
				ServerID:      id,
				LockedHash:    entry.ToolsetHash,
				IntrospectErr: "lockfile missing command for this entry; re-run `ainfra lock`",
			})
			continue
		}

		tools, err := mcpclient.Introspect(context.Background(), mcpclient.Request{
			Command: entry.Command,
			Args:    entry.Args,
			Env:     entry.Env,
			Runner:  runner,
		})
		if err != nil {
			report.Drifts = append(report.Drifts, ToolsetDrift{
				ServerID:      id,
				LockedHash:    entry.ToolsetHash,
				IntrospectErr: err.Error(),
			})
			continue
		}

		liveHash := lockfile.ContentHash(tools)
		if liveHash == entry.ToolsetHash {
			continue // clean
		}
		report.Drifts = append(report.Drifts, ToolsetDrift{
			ServerID:   id,
			LockedHash: entry.ToolsetHash,
			LiveHash:   liveHash,
			Diff:       diffTools(entry.LockedTools, tools),
		})
	}

	return report
}

// mergeMCPEntries returns committed.Entries.MCPServers ∪ personal, with
// personal overriding committed on key collision — same precedence as
// cmd/ainfra.mergeLocks but scoped to MCP entries only.
func mergeMCPEntries(committed, personal *lockfile.Lock) map[string]lockfile.Entry {
	out := map[string]lockfile.Entry{}
	if committed != nil {
		for id, e := range committed.Entries.MCPServers {
			out[id] = e
		}
	}
	if personal != nil {
		for id, e := range personal.Entries.MCPServers {
			out[id] = e
		}
	}
	return out
}

// diffTools compares the locked per-tool fingerprints against a live tool
// list and returns the per-tool changes sorted by tool name. The result is
// empty when locked and live agree on every tool (which should not happen
// when the hashes differ — but if it does, the caller still records drift on
// the hash mismatch and the empty diff is rendered as a generic message).
func diffTools(locked []lockfile.LockedTool, live mcpclient.ToolList) []ToolDiff {
	lockedByName := map[string]lockfile.LockedTool{}
	for _, t := range locked {
		lockedByName[t.Name] = t
	}
	liveByName := map[string]mcpclient.Tool{}
	for _, t := range live {
		liveByName[t.Name] = t
	}

	var diffs []ToolDiff
	// Added or modified.
	for name, lt := range liveByName {
		prev, ok := lockedByName[name]
		if !ok {
			diffs = append(diffs, ToolDiff{Name: name, Kind: ToolAdded})
			continue
		}
		if prev.DescriptionHash != lockfile.ContentHash(lt.Description) {
			diffs = append(diffs, ToolDiff{Name: name, Kind: ToolDescriptionChanged})
		}
		if prev.InputSchemaHash != lockfile.ContentHash(string(lt.InputSchema)) {
			diffs = append(diffs, ToolDiff{Name: name, Kind: ToolInputSchemaChanged})
		}
	}
	// Removed.
	for name := range lockedByName {
		if _, ok := liveByName[name]; !ok {
			diffs = append(diffs, ToolDiff{Name: name, Kind: ToolRemoved})
		}
	}

	sort.Slice(diffs, func(i, j int) bool {
		if diffs[i].Name != diffs[j].Name {
			return diffs[i].Name < diffs[j].Name
		}
		return diffs[i].Kind < diffs[j].Kind
	})
	return diffs
}
