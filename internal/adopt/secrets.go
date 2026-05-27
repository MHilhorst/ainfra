// Package adopt implements brownfield import: reading an existing Claude Code
// repo's .mcp.json, .claude/, and CLAUDE.md and emitting a draft ainfra.yaml.
package adopt

import (
	"crypto/sha1"
	"fmt"
	"regexp"
	"strings"
)

// Warning is a non-fatal note surfaced to the user during a scan.
type Warning struct {
	Message string
}

// stripResult reports the outcome of inspecting one literal value for secret
// material. When Stripped is true, the original value should be replaced with
// a reference to a synthesized secret named SecretName.
type stripResult struct {
	Stripped   bool
	SecretName string
	Original   string
}

// secretPrefixes are the high-confidence credential prefixes adopt strips
// unconditionally. Any value matching one is considered a literal credential.
var secretPrefixes = []string{
	"ghp_",
	"github_pat_",
	"sk-",
	"sk_",
	"xoxb-",
	"xoxp-",
}

// bearerRe matches an Authorization-style "Bearer <token>" value carrying at
// least 20 characters of payload — long enough to plausibly be a real token
// rather than a placeholder like "Bearer test".
var bearerRe = regexp.MustCompile(`^Bearer\s+\S{20,}$`)

// sensitiveKeyRe identifies env / header keys whose value should be treated as
// a credential when it is a literal (not a ${VAR} template).
var sensitiveKeyRe = regexp.MustCompile(`(?i)(token|api[_-]?key|secret|password)`)

// templateRe matches the ${VAR} template form ainfra preserves verbatim.
var templateRe = regexp.MustCompile(`^\$\{[^}]+\}$`)

// inspectValue decides whether value (under key) carries a literal credential
// that adopt should strip. A template-form value is never stripped.
func inspectValue(key, value string) stripResult {
	if value == "" || templateRe.MatchString(value) {
		return stripResult{}
	}
	for _, p := range secretPrefixes {
		if strings.HasPrefix(value, p) {
			return stripResult{Stripped: true, SecretName: secretNameFromKey(key, value), Original: value}
		}
	}
	if bearerRe.MatchString(value) {
		return stripResult{Stripped: true, SecretName: secretNameFromKey(key, value), Original: value}
	}
	if sensitiveKeyRe.MatchString(key) {
		return stripResult{Stripped: true, SecretName: secretNameFromKey(key, value), Original: value}
	}
	return stripResult{}
}

// secretNameFromKey turns an env-var-style key (e.g. GITHUB_TOKEN) into the
// kebab-case name adopt uses for the synthesized secret declaration. When the
// key is generic or empty, a sha1 prefix of the value disambiguates the name.
func secretNameFromKey(key, value string) string {
	name := strings.ToLower(key)
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.Trim(name, "-")
	if name == "" {
		name = "credential"
	}
	if name == "token" || name == "api-key" || name == "apikey" || name == "secret" || name == "password" {
		sum := sha1.Sum([]byte(value))
		name = fmt.Sprintf("%s-%x", name, sum[:4])
	}
	return name
}
