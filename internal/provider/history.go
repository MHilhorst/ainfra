package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// HistoryFile is the name of the apply-history log under .ainfra/. The file is
// append-only JSON Lines: one HistoryEvent per line, oldest first.
const HistoryFile = "history.jsonl"

// HistoryEvent records one mutation an apply made (or failed to make). It is
// the substrate for a future Govern product — drift detection and approval
// flows can read this log without ainfra having to design either today.
//
// A noop change never produces an event; the log records intent that
// materialised (or tried to) and stays clean of "nothing changed" entries.
type HistoryEvent struct {
	TS           string `json:"ts"`
	Actor        string `json:"actor,omitempty"`
	Command      string `json:"command"`
	Agent        string `json:"agent,omitempty"`
	Channel      string `json:"channel"`
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	ManifestHash string `json:"manifestHash,omitempty"`
}

func historyPath(root string) string {
	return filepath.Join(root, ".ainfra", HistoryFile)
}

// AppendHistory appends events to .ainfra/history.jsonl, creating the file and
// the .ainfra/ directory if needed. An empty events slice is a no-op. Each
// event without a TS is stamped with the current UTC time in RFC3339Nano.
func AppendHistory(root string, events []HistoryEvent) error {
	if len(events) == 0 {
		return nil
	}
	dir := filepath.Join(root, ".ainfra")
	if err := ensureDir(dir); err != nil {
		return err
	}
	f, err := os.OpenFile(historyPath(root), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range events {
		if e.TS == "" {
			e.TS = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if err := enc.Encode(&e); err != nil {
			return err
		}
	}
	return nil
}

// ReadHistory loads every event from .ainfra/history.jsonl in file order. A
// missing file returns a nil slice, not an error — a fresh repo has no history.
func ReadHistory(root string) ([]HistoryEvent, error) {
	data, err := os.ReadFile(historyPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []HistoryEvent
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e HistoryEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// kindString maps a ChangeKind to the kebab-case word stored in history.
// ChangeNoop returns "" so callers can drop noops with one check.
func kindString(k ChangeKind) string {
	switch k {
	case ChangeCreate:
		return "create"
	case ChangeUpdate:
		return "update"
	case ChangeDelete:
		return "delete"
	default:
		return ""
	}
}

// EventsFromResults flattens per-channel apply results into history events,
// stamping the channel/id/kind onto a base event the caller has filled with
// ts/actor/command/agent/manifestHash. Noop changes are skipped. Failed and
// blocked changes record the attempted kind suffixed with "-failed" or
// "-skipped" so the log preserves intent even when nothing materialised.
func EventsFromResults(results []ApplyResult, base HistoryEvent) []HistoryEvent {
	var out []HistoryEvent
	for _, r := range results {
		for _, c := range r.Applied {
			kind := kindString(c.Kind)
			if kind == "" {
				continue
			}
			e := base
			e.Channel = r.Channel
			e.ID = c.ID
			e.Kind = kind
			out = append(out, e)
		}
		for _, f := range r.Failed {
			kind := kindString(f.Change.Kind)
			if kind == "" {
				continue
			}
			e := base
			e.Channel = r.Channel
			e.ID = f.Change.ID
			e.Kind = kind + "-failed"
			out = append(out, e)
		}
		for _, s := range r.Skipped {
			kind := kindString(s.Change.Kind)
			if kind == "" {
				continue
			}
			e := base
			e.Channel = r.Channel
			e.ID = s.Change.ID
			e.Kind = kind + "-skipped"
			out = append(out, e)
		}
	}
	return out
}
