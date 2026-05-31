package plugin

import (
	"strings"
	"testing"
)

const sampleMarketplace = `{
  "name": "trein-vertraging",
  "owner": { "name": "Trein-Vertraging" },
  "plugins": [
    { "name": "tvt-config", "source": "./", "description": "human blurb" },
    { "name": "claude-ads", "source": { "source": "github", "repo": "x/y" }, "description": "third party" }
  ]
}`

func TestVerifyMarketplaceEntry_Present(t *testing.T) {
	if err := VerifyMarketplaceEntry([]byte(sampleMarketplace), "tvt-config"); err != nil {
		t.Errorf("expected present entry to verify, got %v", err)
	}
}

func TestVerifyMarketplaceEntry_Missing(t *testing.T) {
	err := VerifyMarketplaceEntry([]byte(sampleMarketplace), "absent")
	if err == nil || !strings.Contains(err.Error(), "no marketplace entry") {
		t.Errorf("expected missing-entry error, got %v", err)
	}
}

func TestVerifyMarketplaceEntry_BadJSON(t *testing.T) {
	if err := VerifyMarketplaceEntry([]byte("{not json"), "x"); err == nil {
		t.Error("expected parse error on invalid json")
	}
}
