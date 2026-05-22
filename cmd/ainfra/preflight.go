package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/MHilhorst/ainfra/internal/manifest"
	"github.com/MHilhorst/ainfra/internal/provider/pkg"
)

// cliToolInstallWarnings returns one warning per cliTool whose install: block
// declares no method ainfra can automate. apply falls back to a bare PATH
// probe for such tools, so a successful lock does not mean apply will install
// them. Entries are de-duplicated across layers and reported in id order.
func cliToolInstallWarnings(layers map[manifest.Layer]*manifest.Manifest) []string {
	var warnings []string
	seen := map[string]bool{}
	for _, ln := range []manifest.Layer{manifest.LayerTeam, manifest.LayerRepo, manifest.LayerPersonal} {
		m, ok := layers[ln]
		if !ok {
			continue
		}
		for _, id := range slices.Sorted(maps.Keys(m.CLITools)) {
			if seen[id] {
				continue
			}
			seen[id] = true
			t := m.CLITools[id]
			automatable := false
			for method := range t.Install {
				if _, ok := pkg.Select(method); ok {
					automatable = true
					break
				}
			}
			if !automatable {
				declared := slices.Sorted(maps.Keys(t.Install))
				declaredStr := strings.Join(declared, ", ")
				if declaredStr == "" {
					declaredStr = "(none)"
				}
				warnings = append(warnings, fmt.Sprintf(
					"cliTool %q declares no install method ainfra can automate "+
						"(declared: %s; automatable: %s) — apply will probe for it "+
						"on PATH and fail if it is not already installed",
					id, declaredStr, strings.Join(pkg.Methods(), ", ")))
			}
		}
	}
	return warnings
}
