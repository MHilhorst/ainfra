// Package lockfile reads, writes, and hashes ai-stack.lock (spec Phase 2).
package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// ContentHash returns a sha256: hash of v in a normalized form: map keys
// sorted, so cosmetic ordering differences are never false drift (spec §5).
func ContentHash(v any) string {
	var b strings.Builder
	writeNormalized(&b, v)
	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeNormalized(b *strings.Builder, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for _, k := range keys {
			b.WriteString(k)
			b.WriteByte(':')
			writeNormalized(b, t[k])
			b.WriteByte(',')
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for _, e := range t {
			writeNormalized(b, e)
			b.WriteByte(',')
		}
		b.WriteByte(']')
	default:
		fmt.Fprintf(b, "%v", t)
	}
}
