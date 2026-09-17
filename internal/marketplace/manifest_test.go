package marketplace

import (
	"encoding/json"
	"testing"

	"github.com/mtfuller/agentworks/internal/importer"
)

func TestUnmarshalSourceStringForm(t *testing.T) {
	var e manifestEntry
	if err := json.Unmarshal([]byte(`{"name":"x","source":"./plugins/x"}`), &e); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !e.Source.isString || e.Source.str != "./plugins/x" {
		t.Errorf("Source = %+v, want a string form of ./plugins/x", e.Source)
	}
}

func TestUnmarshalSourceObjectForm(t *testing.T) {
	var e manifestEntry
	if err := json.Unmarshal([]byte(`{"name":"x","source":{"source":"github","repo":"owner/repo","ref":"v1.0.0"}}`), &e); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if e.Source.isString {
		t.Fatal("Source.isString = true, want object form")
	}
	if e.Source.obj.Kind != "github" || e.Source.obj.Repo != "owner/repo" || e.Source.obj.Ref != "v1.0.0" {
		t.Errorf("Source.obj = %+v", e.Source.obj)
	}
}

func TestResolveRelativePathAgainstMarketplaceRepo(t *testing.T) {
	m := Marketplace{Repo: "anthropics/claude-plugins-official", Ref: "main"}
	e := manifestEntry{Name: "x"}
	if err := json.Unmarshal([]byte(`"./plugins/x"`), &e.Source); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	src, err := e.resolve(m)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	want := importer.Source{Kind: importer.SourceGitHub, Repo: m.Repo, Ref: m.Ref, Path: "plugins/x"}
	if src != want {
		t.Errorf("resolve() = %+v, want %+v", src, want)
	}
}

func TestResolveGitHubSource(t *testing.T) {
	m := Marketplace{Repo: "some/marketplace", Ref: "main"}
	e := manifestEntry{Name: "x"}
	if err := json.Unmarshal([]byte(`{"source":"github","repo":"owner/plugin-repo","ref":"stable"}`), &e.Source); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	src, err := e.resolve(m)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	want := importer.Source{Kind: importer.SourceGitHub, Repo: "owner/plugin-repo", Ref: "stable"}
	if src != want {
		t.Errorf("resolve() = %+v, want %+v", src, want)
	}
}

func TestResolveArchiveSource(t *testing.T) {
	m := Marketplace{Repo: "some/marketplace", Ref: "main"}
	e := manifestEntry{Name: "x"}
	if err := json.Unmarshal([]byte(`{"source":"archive","url":"https://example.com/plugin.zip"}`), &e.Source); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	src, err := e.resolve(m)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	want := importer.Source{Kind: importer.SourceArchive, URL: "https://example.com/plugin.zip"}
	if src != want {
		t.Errorf("resolve() = %+v, want %+v", src, want)
	}
}

func TestResolveUnsupportedTypes(t *testing.T) {
	m := Marketplace{Repo: "some/marketplace", Ref: "main"}
	tests := []string{
		`{"source":"npm","package":"@acme/plugin"}`,
		`{"source":"command","command":"my-tool path"}`,
		`{"source":"url","url":"https://gitlab.com/team/plugin.git"}`,
		`{"source":"git-subdir","url":"https://gitlab.com/team/mono.git","path":"plugin"}`,
	}
	for _, raw := range tests {
		e := manifestEntry{Name: "x"}
		if err := json.Unmarshal([]byte(raw), &e.Source); err != nil {
			t.Fatalf("Unmarshal(%s) error = %v", raw, err)
		}
		if _, err := e.resolve(m); err == nil {
			t.Errorf("resolve(%s) expected error, got nil", raw)
		}
	}
}

func TestResolveGitHubHostedURLAndGitSubdir(t *testing.T) {
	m := Marketplace{Repo: "some/marketplace", Ref: "main"}

	e := manifestEntry{Name: "x"}
	if err := json.Unmarshal([]byte(`{"source":"url","url":"https://github.com/owner/repo.git","ref":"v2"}`), &e.Source); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	src, err := e.resolve(m)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if want := (importer.Source{Kind: importer.SourceGitHub, Repo: "owner/repo", Ref: "v2"}); src != want {
		t.Errorf("resolve() = %+v, want %+v", src, want)
	}

	e2 := manifestEntry{Name: "y"}
	if err := json.Unmarshal([]byte(`{"source":"git-subdir","url":"https://github.com/owner/mono.git","path":"tools/x","ref":"main"}`), &e2.Source); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	src2, err := e2.resolve(m)
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}
	if want := (importer.Source{Kind: importer.SourceGitHub, Repo: "owner/mono", Ref: "main", Path: "tools/x"}); src2 != want {
		t.Errorf("resolve() = %+v, want %+v", src2, want)
	}
}
