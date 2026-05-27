// Package writer applies targeted, comment-preserving edits to ainfra.yaml
// and ainfra.personal.yaml files. The strategy is text surgery, not YAML
// marshal: parse only to locate line positions of channel blocks and their
// child entries, then splice text directly. This guarantees byte-cleanness
// outside the modified region — comments, whitespace, inline-map shorthand,
// and unicode characters are all preserved untouched.
//
// The writer supports three operations:
//
//	AddEntry(path, channel, id, body)   // append a new entry
//	RemoveEntry(path, channel, id)      // delete an existing entry
//	UpdateEntryVersion(path, channel, id, version)  // bump a version pin
//
// All three are no-ops when the targeted state is already in place (idempotent
// or fail-clean). See the typed errors below.
package writer

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Typed errors so callers can distinguish "already there" from "real failure."
var (
	ErrEntryExists   = errors.New("entry already exists")
	ErrEntryNotFound = errors.New("entry not found")
	ErrChannelEmpty  = errors.New("channel exists but has no entries; cannot infer indent")
)

// AddEntry appends a new entry under the named channel in the YAML file at
// path. The body argument is the entry's YAML (without the leading id key) —
// e.g. for `mcpServers.github`, body would be the lines under `github:`. The
// id is added by this function with appropriate indent.
//
// If the channel does not exist, it is appended as a new top-level block.
// If the entry already exists, returns ErrEntryExists without writing.
func AddEntry(path, channel, id, body string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, err := addEntry(raw, channel, id, body)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0o644)
}

// RemoveEntry deletes an entry's lines from the file. Returns ErrEntryNotFound
// if the entry isn't there.
func RemoveEntry(path, channel, id string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated, err := removeEntry(raw, channel, id)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0o644)
}

// addEntry is the testable core; takes and returns []byte so tests don't need
// to touch the filesystem.
func addEntry(raw []byte, channel, id, body string) ([]byte, error) {
	lines := splitKeepNL(raw)

	chRange, indent, err := findChannel(raw, channel)
	if err == errChannelMissing {
		return appendNewChannel(raw, channel, id, body), nil
	}
	if err != nil {
		return nil, err
	}

	// Does the entry already exist? Scan child keys of the channel.
	if existing := findEntryInChannel(raw, channel, id); existing.start >= 0 {
		return nil, fmt.Errorf("%s.%s: %w", channel, id, ErrEntryExists)
	}

	// Build the entry text with proper indent.
	entryText := formatEntry(id, body, indent)

	// Insertion point: at the end of the channel block, just before the next
	// top-level key (or EOF).
	insertAt := chRange.end // line index (exclusive)
	return spliceLines(lines, insertAt, insertAt, entryText), nil
}

// removeEntry deletes the entry from the channel.
func removeEntry(raw []byte, channel, id string) ([]byte, error) {
	lines := splitKeepNL(raw)
	entry := findEntryInChannel(raw, channel, id)
	if entry.start < 0 {
		return nil, fmt.Errorf("%s.%s: %w", channel, id, ErrEntryNotFound)
	}
	return spliceLines(lines, entry.start, entry.end, ""), nil
}

// channelRange is the half-open line range [start, end) of a channel's value
// block, where start is the line immediately after the channel key and end
// is the line index of the next top-level key (or len(lines) for trailing
// channels).
type channelRange struct{ start, end int }

// entryRange is the half-open line range [start, end) covering one entry,
// including the entry's key line and all of its indented children.
type entryRange struct{ start, end int }

var errChannelMissing = errors.New("channel missing")

// findChannel locates the named channel in raw and returns its value block's
// line range plus the indent string used for child entries.
func findChannel(raw []byte, channel string) (channelRange, string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return channelRange{}, "", err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return channelRange{}, "", errChannelMissing
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return channelRange{}, "", errChannelMissing
	}

	// Find the channel key/value pair plus the next top-level key (to bound end).
	for i := 0; i < len(root.Content); i += 2 {
		k := root.Content[i]
		v := root.Content[i+1]
		if k.Value != channel {
			continue
		}
		// Found it. Now find the end line: either the start line of the next
		// top-level key, or the EOF.
		end := -1
		if i+2 < len(root.Content) {
			nextKey := root.Content[i+2]
			end = nextKey.Line - 1 // convert 1-based to 0-based; -1 because we want exclusive
		}
		lines := splitKeepNL(raw)
		if end == -1 {
			end = len(lines)
		}
		start := k.Line // first line of the channel's value (the line after the key, in 1-based... but yaml gives the key line; the value starts there or below)
		// We treat start as the line index immediately AFTER the channel-key line.
		// The channel-key line itself is k.Line (1-based). In 0-based that's k.Line - 1.
		// We want the value block to start at the next line: (k.Line - 1) + 1 = k.Line.
		_ = start

		indent := detectChannelIndent(v)
		return channelRange{start: k.Line, end: end}, indent, nil
	}
	return channelRange{}, "", errChannelMissing
}

// detectChannelIndent looks at the first child of a mapping value and returns
// the leading whitespace of its key line. Returns "  " (two spaces) when the
// channel is empty or scalar.
func detectChannelIndent(v *yaml.Node) string {
	if v == nil || v.Kind != yaml.MappingNode || len(v.Content) == 0 {
		return "  "
	}
	firstKey := v.Content[0]
	// Column is 1-based. Indent string is (column - 1) spaces.
	if firstKey.Column < 2 {
		return "  "
	}
	return strings.Repeat(" ", firstKey.Column-1)
}

// findEntryInChannel scans the channel's value block for a child key matching
// id and returns its line range. Returns {-1,-1} when not found.
func findEntryInChannel(raw []byte, channel, id string) entryRange {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return entryRange{-1, -1}
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return entryRange{-1, -1}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return entryRange{-1, -1}
	}
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value != channel {
			continue
		}
		val := root.Content[i+1]
		if val.Kind != yaml.MappingNode {
			return entryRange{-1, -1}
		}
		lines := splitKeepNL(raw)
		// Find the entry key in val.Content and the next sibling (or end of
		// channel block) for the end line.
		for j := 0; j < len(val.Content); j += 2 {
			k := val.Content[j]
			if k.Value != id {
				continue
			}
			startLine := k.Line - 1 // 0-based
			endLine := -1
			if j+2 < len(val.Content) {
				next := val.Content[j+2]
				endLine = next.Line - 1
			} else {
				// Last entry — find end-of-channel block.
				if i+2 < len(root.Content) {
					nextTop := root.Content[i+2]
					endLine = nextTop.Line - 1
				} else {
					endLine = len(lines)
				}
			}
			return entryRange{start: startLine, end: endLine}
		}
		return entryRange{-1, -1}
	}
	return entryRange{-1, -1}
}

// formatEntry renders the entry id + body with the given indent applied to the
// entry's key. The body is treated as already-indented YAML for the entry's
// value — each non-empty line gets prefixed with indent+indent (one extra
// level beyond the key).
func formatEntry(id, body, indent string) string {
	var b strings.Builder
	b.WriteString(indent)
	b.WriteString(id)
	b.WriteString(":\n")
	bodyLines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	childIndent := indent + indent // double the channel indent for grandchildren
	for _, l := range bodyLines {
		if l == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(childIndent)
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

// appendNewChannel adds a brand-new channel block at the end of the file.
func appendNewChannel(raw []byte, channel, id, body string) []byte {
	out := make([]byte, 0, len(raw)+len(body)+64)
	out = append(out, raw...)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	out = append(out, '\n')
	out = append(out, channel...)
	out = append(out, ":\n"...)
	out = append(out, formatEntry(id, body, "  ")...)
	return out
}

// splitKeepNL splits raw on '\n' but keeps the trailing newline on each line
// so reassembly is exact.
func splitKeepNL(raw []byte) []string {
	s := string(raw)
	var lines []string
	for {
		nl := strings.IndexByte(s, '\n')
		if nl < 0 {
			if s != "" {
				lines = append(lines, s)
			}
			return lines
		}
		lines = append(lines, s[:nl+1])
		s = s[nl+1:]
	}
}

// spliceLines replaces lines[from:to] with insert (a multi-line string).
// from and to are 0-based; to is exclusive.
func spliceLines(lines []string, from, to int, insert string) []byte {
	var b strings.Builder
	for i := 0; i < from; i++ {
		b.WriteString(lines[i])
	}
	b.WriteString(insert)
	for i := to; i < len(lines); i++ {
		b.WriteString(lines[i])
	}
	return []byte(b.String())
}
