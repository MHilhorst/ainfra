package ui

import (
	"bytes"
	"testing"
)

func TestColorizerDisabledByDefaultForNonTerminal(t *testing.T) {
	// A bytes.Buffer is not a terminal, so color must be off.
	c := NewColorizer(&bytes.Buffer{}, false)
	if got := c.Green("+"); got != "+" {
		t.Errorf("disabled Green(%q) = %q, want %q", "+", got, "+")
	}
}

func TestColorizerForceOff(t *testing.T) {
	c := NewColorizer(&bytes.Buffer{}, true)
	if got := c.Red("x"); got != "x" {
		t.Errorf("force-off Red = %q, want %q", got, "x")
	}
}

func TestColorizerDisabledByNoColorEnv(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	c := NewColorizer(&bytes.Buffer{}, false)
	if got := c.Green("+"); got != "+" {
		t.Errorf("NO_COLOR Green(%q) = %q, want %q", "+", got, "+")
	}
}

func TestColorizerEnabledWraps(t *testing.T) {
	c := Colorizer{enabled: true}
	got := c.Green("+")
	want := "\033[32m+\033[0m"
	if got != want {
		t.Errorf("enabled Green(+) = %q, want %q", got, want)
	}
}
