// Package lockfile reads, writes, and hashes ai-stack.lock (spec Phase 2).
package lockfile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ContentHash returns a sha256: hash of v in a normalized form. It uses JSON
// encoding, which sorts map keys (so cosmetic key ordering is never false
// drift), quotes and escapes strings (so structurally different values cannot
// collide), and preserves type distinctions (the int 1 and the string "1"
// hash differently). See spec §5.
func ContentHash(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		// v holds a type JSON cannot encode — a programming error. Hash the
		// error text so the result stays deterministic instead of panicking.
		data = []byte("aistack:unhashable:" + err.Error())
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
