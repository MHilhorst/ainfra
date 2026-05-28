package manifest

import (
	"os"
	"testing"
)

// TestMain isolates XDG_CONFIG_HOME so a developer's real personal manifest
// at ~/.config/ainfra/personal.yaml does not leak into LoadLayers fixtures
// and turn TestLoadLayersPersonalOptional (and others) into machine-dependent
// flakes.
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "ainfra-test-xdg-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)
	os.Setenv("XDG_CONFIG_HOME", tmp)
	os.Exit(m.Run())
}
