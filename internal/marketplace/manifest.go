package marketplace

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mtfuller/agentworks/internal/importer"
)

// manifestDoc is the (partial) marketplace.json schema -- only the fields
// AgentWorks actually uses.
type manifestDoc struct {
	Name    string          `json:"name"`
	Plugins []manifestEntry `json:"plugins"`
}

type manifestEntry struct {
	Name        string     `json:"name"`
	DisplayName string     `json:"displayName"`
	Description string     `json:"description"`
	Version     string     `json:"version"`
	Category    string     `json:"category"`
	Homepage    string     `json:"homepage"`
	Keywords    []string   `json:"keywords"`
	Tags        []string   `json:"tags"`
	Author      flexString `json:"author"`
	License     flexString `json:"license"`
	Source      rawSource  `json:"source"`
}

// flexString decodes a JSON field that marketplaces write either as a bare
// string ("MIT") or as an object ({"name": "MIT"} / {"type": "MIT"}) --
// author and license both show up in both shapes in the wild.
type flexString string

func (f *flexString) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var obj struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		*f = "" // an unrecognizable shape is "unknown", not a parse failure
		return nil
	}
	if obj.Name != "" {
		*f = flexString(obj.Name)
	} else {
		*f = flexString(obj.Type)
	}
	return nil
}

func (e manifestEntry) displayName() string {
	if e.DisplayName != "" {
		return e.DisplayName
	}
	return e.Name
}

// rawSource handles marketplace.json's source field being either a bare
// string (a relative path resolved against the marketplace's own repo) or
// an object ({"source": "github"|"url"|"git-subdir"|"npm"|"archive"|"command", ...}).
type rawSource struct {
	isString bool
	str      string
	obj      sourceObject
}

type sourceObject struct {
	Kind string `json:"source"`
	Repo string `json:"repo"` // github
	URL  string `json:"url"`  // url, git-subdir, archive
	Path string `json:"path"` // git-subdir
	Ref  string `json:"ref"`  // github, url, git-subdir
}

func (r *rawSource) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		r.isString = true
		return json.Unmarshal(data, &r.str)
	}
	r.isString = false
	return json.Unmarshal(data, &r.obj)
}

// resolve turns one marketplace.json plugin entry into an importer.Source.
// A bare-string source is a relative path resolved against m's own repo;
// "github" and "archive" objects map straight onto importer's two source
// kinds; "url"/"git-subdir" (arbitrary git remotes) only resolve when the
// URL happens to be github.com, since importer has no general git-clone
// capability; "npm"/"command"/anything else is a clear per-entry error.
func (e manifestEntry) resolve(m Marketplace) (importer.Source, error) {
	if e.Source.isString {
		p := strings.TrimPrefix(strings.TrimPrefix(e.Source.str, "./"), "/")
		return importer.Source{Kind: importer.SourceGitHub, Repo: m.Repo, Ref: m.Ref, Path: p}, nil
	}

	obj := e.Source.obj
	switch obj.Kind {
	case "github":
		return importer.Source{Kind: importer.SourceGitHub, Repo: obj.Repo, Ref: obj.Ref}, nil
	case "archive":
		return importer.Source{Kind: importer.SourceArchive, URL: obj.URL}, nil
	case "url":
		if repo, ok := githubRepoFromGitURL(obj.URL); ok {
			return importer.Source{Kind: importer.SourceGitHub, Repo: repo, Ref: obj.Ref}, nil
		}
		return importer.Source{}, fmt.Errorf("%s: only GitHub-hosted git URLs are supported yet (got %s)", e.displayName(), obj.URL)
	case "git-subdir":
		if repo, ok := githubRepoFromGitURL(obj.URL); ok {
			return importer.Source{Kind: importer.SourceGitHub, Repo: repo, Ref: obj.Ref, Path: obj.Path}, nil
		}
		return importer.Source{}, fmt.Errorf("%s: only GitHub-hosted git-subdir sources are supported yet (got %s)", e.displayName(), obj.URL)
	default:
		return importer.Source{}, fmt.Errorf("%s: %q sources aren't supported yet", e.displayName(), obj.Kind)
	}
}

func githubRepoFromGitURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host != "github.com" {
		return "", false
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) < 2 {
		return "", false
	}
	return segs[0] + "/" + strings.TrimSuffix(segs[1], ".git"), true
}
