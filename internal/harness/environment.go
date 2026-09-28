package harness

import (
	"bytes"
	"sort"
	"strings"

	awprocess "github.com/mtfuller/agentworks/internal/process"
)

var operationalEnvironment = map[string]bool{
	"APPDATA": true, "CI": true, "CLAUDE_CONFIG_DIR": true, "COLORTERM": true,
	"COMSPEC": true, "COPILOT_HOME": true, "HOME": true, "LANG": true,
	"LOCALAPPDATA": true, "NO_COLOR": true, "NODE_EXTRA_CA_CERTS": true,
	"PATH": true, "PATHEXT": true, "SHELL": true, "SSL_CERT_DIR": true,
	"SSL_CERT_FILE": true, "SYSTEMROOT": true, "TEMP": true, "TERM": true,
	"TMP": true, "TMPDIR": true, "USER": true, "USERPROFILE": true,
	"USERNAME": true, "XDG_CACHE_HOME": true, "XDG_CONFIG_HOME": true,
	"XDG_DATA_HOME": true, "XDG_RUNTIME_DIR": true,
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true,
	"http_proxy": true, "https_proxy": true, "no_proxy": true,
}

// RedactEnvironmentSecrets removes exact recognized credential values after a
// harness's protocol filter has normalized output. This is defense in depth
// for vendor diagnostics; short values are ignored to avoid corrupting normal
// output through broad replacement.
func RedactEnvironmentSecrets(inner awprocess.Filter, environment []string, credentialNames ...string) awprocess.Filter {
	allowed := map[string]bool{}
	for _, name := range credentialNames {
		allowed[name] = true
	}
	secrets := [][]byte{}
	seen := map[string]bool{}
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if !found || !allowed[name] || len(value) < 8 || seen[value] {
			continue
		}
		seen[value] = true
		secrets = append(secrets, []byte(value))
	}
	sort.Slice(secrets, func(i, j int) bool { return len(secrets[i]) > len(secrets[j]) })
	return func(stream awprocess.Stream, chunk []byte) []byte {
		if inner != nil {
			chunk = inner(stream, chunk)
		}
		for _, secret := range secrets {
			chunk = bytes.ReplaceAll(chunk, secret, []byte("[REDACTED]"))
		}
		return chunk
	}
}

// RuntimeEnvironment returns the smallest host environment needed to launch a
// harness. Callers explicitly name any credential variables the selected
// harness supports; unrelated host secrets are never inherited by a run.
func RuntimeEnvironment(source []string, credentialNames ...string) []string {
	allowed := make(map[string]bool, len(operationalEnvironment)+len(credentialNames))
	for name := range operationalEnvironment {
		allowed[name] = true
	}
	for _, name := range credentialNames {
		allowed[name] = true
	}

	values := map[string]string{}
	for _, entry := range source {
		name, _, found := strings.Cut(entry, "=")
		if !found || name == "" {
			continue
		}
		if allowed[name] || strings.HasPrefix(name, "LC_") {
			values[name] = entry
		}
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, values[name])
	}
	return result
}
