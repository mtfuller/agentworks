package marketplace

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/mtfuller/agentworks/internal/importer"
)

// commerciallySafe is the set of SPDX identifiers a plugin's license must
// resolve to for the TUI to offer it: permissive licenses that allow
// commercial use, modification, and redistribution without copyleft
// obligations on the code that uses them. Copyleft (GPL/AGPL/LGPL/MPL),
// non-commercial (CC-BY-NC), source-available, and unlicensed code are
// all deliberately excluded.
var commerciallySafe = map[string]bool{
	"MIT":          true,
	"APACHE-2.0":   true,
	"BSD-2-CLAUSE": true,
	"BSD-3-CLAUSE": true,
	"ISC":          true,
	"0BSD":         true,
	"UNLICENSE":    true,
	"CC0-1.0":      true,
	"ZLIB":         true,
}

// IsCommerciallySafe reports whether an SPDX license expression is
// permissive enough to use commercially. "A OR B" is safe if either side
// is (the licensee picks); "A AND B" only if both are.
func IsCommerciallySafe(expr string) bool {
	expr = strings.TrimSpace(strings.Trim(strings.TrimSpace(expr), "()"))
	if expr == "" {
		return false
	}
	if parts := strings.Split(expr, " OR "); len(parts) > 1 {
		for _, p := range parts {
			if IsCommerciallySafe(p) {
				return true
			}
		}
		return false
	}
	if parts := strings.Split(expr, " AND "); len(parts) > 1 {
		for _, p := range parts {
			if !IsCommerciallySafe(p) {
				return false
			}
		}
		return true
	}
	return commerciallySafe[normalizeLicense(expr)]
}

// normalizeLicense maps the common spellings marketplaces use ("Apache
// 2.0", "apache-2.0", "MIT License") onto the upper-cased SPDX id used as a
// commerciallySafe key. Unrecognized values pass through upper-cased, so
// they simply fail the allowlist lookup.
func normalizeLicense(s string) string {
	u := strings.ToUpper(strings.TrimSpace(s))
	u = strings.TrimSuffix(u, " LICENSE")
	switch u {
	case "APACHE 2.0", "APACHE2", "APACHE-2", "APACHE LICENSE 2.0", "APACHE LICENSE, VERSION 2.0", "APACHE2.0":
		return "APACHE-2.0"
	case "BSD", "BSD-3", "BSD 3-CLAUSE", "NEW BSD":
		return "BSD-3-CLAUSE"
	case "BSD-2", "BSD 2-CLAUSE", "SIMPLIFIED BSD":
		return "BSD-2-CLAUSE"
	case "THE UNLICENSE":
		return "UNLICENSE"
	}
	return u
}

// detectLicense sniffs a LICENSE file's text for the distinctive phrase of
// each allowlisted license and returns its SPDX id, or "" if it isn't one
// (which includes every copyleft license -- none of their text contains
// these phrases).
func detectLicense(text string) string {
	t := strings.Join(strings.Fields(strings.ToLower(text)), " ")
	switch {
	case strings.Contains(t, "apache license") && strings.Contains(t, "version 2.0"):
		return "Apache-2.0"
	case strings.Contains(t, "permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(t, "this is free and unencumbered software released into the public domain"):
		return "Unlicense"
	case strings.Contains(t, "permission to use, copy, modify, and/or distribute this software for any purpose with or without fee"):
		return "ISC"
	case strings.Contains(t, "redistribution and use in source and binary forms"):
		if strings.Contains(t, "neither the name") {
			return "BSD-3-Clause"
		}
		return "BSD-2-Clause"
	}
	return ""
}

// licenseFileNames are tried, in order, at a plugin's own directory and
// then at its repo root.
var licenseFileNames = []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "COPYING"}

// licenseCache remembers lookups for the life of the process, keyed by
// repo+ref+path, so refreshing the pane (or two plugins sharing a
// monorepo) doesn't re-fetch the same LICENSE file.
var licenseCache sync.Map // string -> string

// lookupLicense finds the SPDX id of the license governing src by reading
// its LICENSE file from raw.githubusercontent.com -- the plugin's own
// directory first (a monorepo plugin can carry its own), then the repo
// root. Only GitHub sources can be checked; anything else, or a repo with
// no recognizable permissive LICENSE, returns "".
func lookupLicense(ctx context.Context, src importer.Source) string {
	if src.Kind != importer.SourceGitHub {
		return ""
	}
	ref := src.Ref
	if ref == "" {
		ref = "HEAD"
	}
	key := src.Repo + "@" + ref + "/" + src.Path
	if v, ok := licenseCache.Load(key); ok {
		return v.(string)
	}

	dirs := []string{""}
	if src.Path != "" {
		dirs = []string{strings.Trim(src.Path, "/") + "/", ""}
	}
	found := ""
search:
	for _, dir := range dirs {
		for _, name := range licenseFileNames {
			text, ok := fetchText(ctx, rawGitHubBase+"/"+src.Repo+"/"+ref+"/"+dir+name)
			if !ok {
				continue
			}
			found = detectLicense(text)
			break search // a LICENSE file exists here; it decides, even if not permissive
		}
	}
	licenseCache.Store(key, found)
	return found
}

// fetchText GETs url and returns up to 64KiB of its body on a 200.
func fetchText(ctx context.Context, url string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return "", false
	}
	return string(b), true
}
