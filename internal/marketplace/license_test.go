package marketplace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mtfuller/agentworks/internal/importer"
)

func TestIsCommerciallySafe(t *testing.T) {
	tests := map[string]bool{
		"MIT":                    true,
		"mit":                    true,
		"MIT License":            true,
		"Apache-2.0":             true,
		"Apache 2.0":             true,
		"BSD-3-Clause":           true,
		"ISC":                    true,
		"Unlicense":              true,
		"MIT OR GPL-3.0":         true, // licensee may pick MIT
		"(MIT OR Apache-2.0)":    true,
		"MIT AND GPL-3.0":        false,
		"GPL-3.0":                false,
		"AGPL-3.0-only":          false,
		"LGPL-2.1":               false,
		"MPL-2.0":                false,
		"CC-BY-NC-4.0":           false,
		"Proprietary":            false,
		"SEE LICENSE IN LICENSE": false,
		"":                       false,
	}
	for in, want := range tests {
		if got := IsCommerciallySafe(in); got != want {
			t.Errorf("IsCommerciallySafe(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDetectLicense(t *testing.T) {
	tests := map[string]struct{ text, want string }{
		"mit":         {"MIT License\n\nCopyright (c) 2025 X\n\nPermission is hereby granted, free of charge, to any person obtaining a copy", "MIT"},
		"apache":      {"                                 Apache License\n                           Version 2.0, January 2004", "Apache-2.0"},
		"bsd3":        {"Redistribution and use in source and binary forms, with or without modification, are permitted... Neither the name of the copyright holder", "BSD-3-Clause"},
		"bsd2":        {"Redistribution and use in source and binary forms, with or without modification, are permitted", "BSD-2-Clause"},
		"isc":         {"Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted", "ISC"},
		"unlicense":   {"This is free and unencumbered software released into the public domain.", "Unlicense"},
		"gpl":         {"GNU GENERAL PUBLIC LICENSE Version 3, 29 June 2007", ""},
		"proprietary": {"All rights reserved.", ""},
	}
	for name, tt := range tests {
		if got := detectLicense(tt.text); got != tt.want {
			t.Errorf("%s: detectLicense() = %q, want %q", name, got, tt.want)
		}
	}
}

const mitText = "MIT License\n\nPermission is hereby granted, free of charge, to any person"
const gplText = "GNU GENERAL PUBLIC LICENSE\nVersion 3"

// licenseServer serves a marketplace manifest plus per-repo LICENSE files,
// standing in for raw.githubusercontent.com.
func licenseServer(t *testing.T, manifest string, files map[string]string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "marketplace.json") {
			w.Write([]byte(manifest))
			return
		}
		if body, ok := files[r.URL.Path]; ok {
			w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	}))
	origClient, origRaw, origWK := httpClient, rawGitHubBase, WellKnown
	httpClient, rawGitHubBase = srv.Client(), srv.URL
	WellKnown = []Marketplace{{ID: "t", Name: "Test", Repo: "market/repo", Ref: "main", Path: "marketplace.json"}}
	licenseCache.Range(func(k, _ any) bool { licenseCache.Delete(k); return true })
	t.Cleanup(func() {
		httpClient, rawGitHubBase, WellKnown = origClient, origRaw, origWK
		srv.Close()
	})
}

func TestSearchCommercialOnlyKeepsOnlyPermissiveLicenses(t *testing.T) {
	licenseServer(t, `{"plugins":[
	  {"name":"declared-mit","description":"d","license":"MIT","source":{"source":"github","repo":"a/declared"}},
	  {"name":"declared-gpl","description":"d","license":"GPL-3.0","source":{"source":"github","repo":"a/gpl"}},
	  {"name":"file-mit","description":"d","source":{"source":"github","repo":"a/filemit"}},
	  {"name":"file-gpl","description":"d","source":{"source":"github","repo":"a/filegpl"}},
	  {"name":"unlicensed","description":"d","source":{"source":"github","repo":"a/none"}},
	  {"name":"subdir-own","description":"d","source":{"source":"git-subdir","url":"https://github.com/a/mono.git","path":"plugins/x"}},
	  {"name":"object-license","description":"d","license":{"type":"Apache-2.0"},"source":{"source":"github","repo":"a/obj"}}
	]}`, map[string]string{
		"/a/filemit/HEAD/LICENSE":           mitText,
		"/a/filegpl/HEAD/LICENSE":           gplText,
		"/a/mono/HEAD/plugins/x/LICENSE.md": mitText,
	})

	results, errs := Search(context.Background(), "", Options{CommercialOnly: true})
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	got := map[string]string{}
	for _, r := range results {
		got[r.Name] = r.License
	}
	want := map[string]string{"declared-mit": "MIT", "file-mit": "MIT", "subdir-own": "MIT", "object-license": "Apache-2.0"}
	if len(got) != len(want) {
		t.Fatalf("results = %v, want exactly %v", got, want)
	}
	for name, lic := range want {
		if got[name] != lic {
			t.Errorf("%s license = %q, want %q (all: %v)", name, got[name], lic, got)
		}
	}
}

func TestSearchCommercialOnlySkipsAgentSkills(t *testing.T) {
	withMarketplaceTestServers(t, `{"plugins":[]}`, testAgentSkillsJSON, http.StatusOK, http.StatusOK)

	results, _ := Search(context.Background(), "", Options{CommercialOnly: true})
	if len(results) != 0 {
		t.Errorf("results = %v; agentskills.codes exposes no license and must be excluded", results)
	}
	results, _ = Search(context.Background(), "", Options{})
	if len(results) != 1 {
		t.Errorf("unfiltered Search should still include agentskills.codes, got %v", results)
	}
}

func TestLookupLicenseIgnoresNonGitHubSources(t *testing.T) {
	if got := lookupLicense(context.Background(), importer.Source{Kind: importer.SourceArchive, URL: "https://x/y.zip"}); got != "" {
		t.Errorf("lookupLicense(archive) = %q, want empty", got)
	}
}
