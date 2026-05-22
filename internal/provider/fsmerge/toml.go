package fsmerge

import (
	"errors"
	iofs "io/fs"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// MergeTOMLTables performs a three-way merge of a single TOML table key into
// the file at path. It reads the file (treating a missing file as empty),
// ensures the topKey table exists, removes every key listed in ownedKeys,
// sets every entry from desired, then writes the document back as TOML.
//
// Foreign keys — those present in the file but absent from ownedKeys — are
// preserved as data. Comments and exact formatting are not preserved: the
// document is re-serialised. A file that is not valid TOML is a hard error.
func MergeTOMLTables(fs FS, path, topKey string, desired map[string]any, ownedKeys []string) error {
	raw, err := fs.ReadFile(path)
	if errors.Is(err, iofs.ErrNotExist) {
		raw = []byte{}
	} else if err != nil {
		return err
	}

	doc := map[string]any{}
	if len(raw) > 0 {
		if err := toml.Unmarshal(raw, &doc); err != nil {
			return err
		}
	}

	top, ok := doc[topKey].(map[string]any)
	if !ok {
		top = map[string]any{}
	}

	for _, k := range ownedKeys {
		delete(top, k)
	}
	for k, v := range desired {
		top[k] = v
	}

	if len(top) == 0 {
		delete(doc, topKey)
	} else {
		doc[topKey] = top
	}

	out, err := toml.Marshal(doc)
	if err != nil {
		return err
	}

	if err := fs.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fs.WriteFile(path, out, 0o644)
}
