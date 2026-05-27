package adopt

import "testing"

func TestInspectValue(t *testing.T) {
	cases := []struct {
		name         string
		key          string
		value        string
		wantStripped bool
	}{
		{"ghp prefix", "FOO", "ghp_abcdefghijklmnop", true},
		{"github pat", "X", "github_pat_xxxxxxxxxxxxxxxxxxxx", true},
		{"openai sk-", "OPENAI", "sk-abcdefghijklmnopqrst", true},
		{"openai sk_", "OPENAI", "sk_abcdefghijklmnopqrst", true},
		{"slack xoxb", "TOKEN", "xoxb-1-2-3", true},
		{"slack xoxp", "TOKEN", "xoxp-1-2-3", true},
		{"bearer long", "Authorization", "Bearer abcdefghijklmnopqrstuvwxyz", true},
		{"bearer short", "Authorization", "Bearer test", false},
		{"sensitive key literal", "GITHUB_TOKEN", "literalvalue", true},
		{"api_key literal", "API_KEY", "literal", true},
		{"password literal", "DB_PASSWORD", "hunter2", true},
		{"template preserved", "GITHUB_TOKEN", "${env:GH}", false},
		{"non-sensitive key literal", "PORT", "8080", false},
		{"empty value", "TOKEN", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := inspectValue(tc.key, tc.value)
			if res.Stripped != tc.wantStripped {
				t.Errorf("inspectValue(%q,%q): stripped=%v want %v", tc.key, tc.value, res.Stripped, tc.wantStripped)
			}
		})
	}
}

func TestSecretNameFromKey(t *testing.T) {
	cases := []struct {
		key, value, want string
	}{
		{"GITHUB_TOKEN", "x", "github-token"},
		{"API_KEY", "x", "api-key"}, // generic — gets hash suffix? Actually api-key is in the generic list.
		{"OPENAI_API_KEY", "x", "openai-api-key"},
		{"", "x", "credential"},
	}
	for _, tc := range cases {
		got := secretNameFromKey(tc.key, tc.value)
		if tc.key == "API_KEY" {
			// generic name — should have a hash suffix; only check prefix.
			if got[:7] != "api-key" {
				t.Errorf("secretNameFromKey(%q): got %q, want prefix api-key", tc.key, got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("secretNameFromKey(%q): got %q want %q", tc.key, got, tc.want)
		}
	}
}
