package fsmerge

import (
	"errors"
	iofs "io/fs"
	"path/filepath"
	"sort"
	"strings"
)

const (
	regionBegin = "<!-- ainfra:begin -->"
	regionEnd   = "<!-- ainfra:end -->"
	ruleOpen    = "<!-- ainfra:rule "
	ruleClose   = " -->"
)

// splitRegion divides file content into the text before the ainfra-managed
// region, the per-rule blocks inside it, and the text after. found reports
// whether a region was present. A begin marker with no end marker is an error.
func splitRegion(content string) (before string, rules map[string]string, after string, found bool, err error) {
	bi := strings.Index(content, regionBegin)
	if bi < 0 {
		return content, map[string]string{}, "", false, nil
	}
	ei := strings.Index(content, regionEnd)
	if ei < 0 || ei < bi {
		return "", nil, "", false, errors.New("fsmerge: managed region has a begin marker but no end marker")
	}
	before = content[:bi]
	after = content[ei+len(regionEnd):]
	inner := content[bi+len(regionBegin) : ei]
	return before, parseRuleBlocks(inner), after, true, nil
}

// parseRuleBlocks parses the inside of a managed region into id->content.
// A rule's content runs from its marker line to the next marker (or the end).
func parseRuleBlocks(inner string) map[string]string {
	rules := map[string]string{}
	id := ""
	var body []string
	flush := func() {
		if id != "" {
			rules[id] = strings.Trim(strings.Join(body, "\n"), "\n")
		}
	}
	for _, line := range strings.Split(inner, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, ruleOpen) && strings.HasSuffix(t, ruleClose) {
			flush()
			id = strings.TrimSpace(t[len(ruleOpen) : len(t)-len(ruleClose)])
			body = nil
			continue
		}
		if id != "" {
			body = append(body, line)
		}
	}
	flush()
	return rules
}

// renderRegion renders the managed region for id->content, ids sorted.
func renderRegion(rules map[string]string) string {
	ids := make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var b strings.Builder
	b.WriteString(regionBegin + "\n")
	for _, id := range ids {
		b.WriteString(ruleOpen + id + ruleClose + "\n")
		b.WriteString(rules[id] + "\n")
	}
	b.WriteString(regionEnd)
	return b.String()
}

// ManagedRegionIDs returns the sorted ids of the rules in the ainfra-managed
// region of the file at path. A missing file or absent region returns no ids.
func ManagedRegionIDs(fs FS, path string) ([]string, error) {
	raw, err := fs.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, rules, _, _, err := splitRegion(string(raw))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// MergeManagedRegion updates the ainfra-managed region in the file at path: it
// removes every id in ownedIDs, then sets every id->content pair in blocks.
// Content outside the region is preserved. When the region would become empty
// it is removed entirely, markers and all. A missing file is created.
func MergeManagedRegion(fs FS, path string, blocks map[string]string, ownedIDs []string) error {
	raw, err := fs.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		raw = []byte{}
	} else if err != nil {
		return err
	}

	before, rules, after, found, err := splitRegion(string(raw))
	if err != nil {
		return err
	}
	for _, id := range ownedIDs {
		delete(rules, id)
	}
	for id, content := range blocks {
		rules[id] = content
	}

	var out string
	if len(rules) == 0 {
		// No region content: the file is just the user's text.
		head := strings.TrimRight(before, "\n")
		tail := strings.TrimLeft(after, "\n")
		out = head
		if tail != "" {
			if out != "" {
				out += "\n"
			}
			out += tail
		}
	} else {
		region := renderRegion(rules)
		head := strings.TrimRight(before, "\n")
		tail := strings.TrimLeft(after, "\n")
		if !found {
			// No region yet: append after all existing content.
			head = strings.TrimRight(string(raw), "\n")
			tail = ""
		}
		out = region
		if head != "" {
			out = head + "\n\n" + region
		}
		if tail != "" {
			out += "\n\n" + tail
		}
	}
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}

	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fs.WriteFile(path, []byte(out), 0o644)
}
