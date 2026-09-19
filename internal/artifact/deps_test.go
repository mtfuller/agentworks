package artifact

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func art(kind Kind, ns, name string, requires ...string) *Artifact {
	a := &Artifact{Dir: string(kind) + "/" + name}
	a.Kind, a.Namespace, a.Name = kind, ns, name
	if len(requires) > 0 {
		a.Extra = map[string]any{"requires": requires}
	}
	return a
}

func TestParseRef(t *testing.T) {
	for in, want := range map[string]Ref{
		"skill:csv":         {KindSkill, "", "csv"},
		"mcp:team-a/jira":   {KindMCP, "team-a", "jira"},
		"agent:@team-a/bot": {KindAgent, "team-a", "bot"},
	} {
		got, err := ParseRef(in)
		if err != nil || got != want {
			t.Errorf("ParseRef(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"csv", "skill:", "widget:x", "skill:Has Caps"} {
		if _, err := ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) should fail", bad)
		}
	}
}

func TestCheckRequiresFindsDanglingKindMismatchAndCycles(t *testing.T) {
	arts := []*Artifact{
		art(KindAgent, "", "bot", "skill:csv", "mcp:csv"),
		art(KindSkill, "", "csv"),
		art(KindSkill, "", "a", "skill:b"),
		art(KindSkill, "", "b", "skill:a"),
	}
	joined := strings.Join(CheckRequires(arts), "\n")
	if !strings.Contains(joined, "requires mcp:csv, which doesn't exist") || !strings.Contains(joined, "did you mean skill:csv") {
		t.Errorf("dangling reference not reported with a hint:\n%s", joined)
	}
	if strings.Contains(joined, "requires skill:csv") {
		t.Errorf("a satisfied reference was reported:\n%s", joined)
	}
	if !strings.Contains(joined, "dependency cycle: skill:a -> skill:b -> skill:a") {
		t.Errorf("cycle not reported:\n%s", joined)
	}
}

func TestMissingAndBodyWithRequirements(t *testing.T) {
	bot := art(KindAgent, "", "bot", "skill:csv", "mcp:jira")
	bot.Body = "Do things."
	csv := art(KindSkill, "", "csv")
	if m := bot.Missing([]*Artifact{bot, csv}); len(m) != 1 || m[0].String() != "mcp:jira" {
		t.Errorf("Missing = %v", m)
	}
	body := bot.BodyWithRequirements()
	if !strings.Contains(body, "## Requires") || !strings.Contains(body, "skill `csv`") || !strings.Contains(body, "mcp `jira`") {
		t.Errorf("body = %q", body)
	}
	if csv.BodyWithRequirements() != csv.Body {
		t.Error("only agents get a Requires section")
	}
}

func TestBinRequirements(t *testing.T) {
	b, err := ParseBin("node>=20")
	if err != nil || b.Name != "node" || b.MinVersion != "20" {
		t.Fatalf("ParseBin = %v, %v", b, err)
	}
	if _, err := ParseBin("node >= 20"); err == nil {
		t.Error("spaces should be rejected")
	}
	if CompareVersions("3.10.2", "3.9") <= 0 || CompareVersions("3.9", "3.9.0") != 0 || CompareVersions("1", "2") >= 0 {
		t.Error("CompareVersions is wrong")
	}
	if msg := CheckBin(BinRequirement{Name: "definitely-not-a-real-binary-xyz"}); !strings.Contains(msg, "not on PATH") {
		t.Errorf("missing binary: %q", msg)
	}
	if msg := CheckBin(BinRequirement{Name: "sh"}); msg != "" {
		t.Errorf("sh should be satisfied, got %q", msg)
	}
}

func TestSyntaxErrorsForRequiresAndBins(t *testing.T) {
	a := art(KindAgent, "", "bot", "skill:csv", "not-a-ref", "agent:bot")
	a.Extra["bins"] = []string{"node>=20", "node >= 20"}
	if errs := a.RequiresSyntaxErrors(); len(errs) != 2 {
		t.Errorf("RequiresSyntaxErrors = %v, want the malformed one and the self-reference", errs)
	}
	if errs := a.BinsSyntaxErrors(); len(errs) != 1 {
		t.Errorf("BinsSyntaxErrors = %v, want just the spaced one", errs)
	}
	if got := a.Bins(); len(got) != 1 || got[0].MinVersion != "20" {
		t.Errorf("Bins = %v", got)
	}
}

func TestCheckBinComparesVersions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the fake binary")
	}
	dir := t.TempDir()
	script := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script("fakebin", `echo "fakebin version 3.10.2"`)
	script("nover", `echo "no digits here"`)
	script("broken", `exit 1`)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if msg := CheckBin(BinRequirement{Name: "fakebin", MinVersion: "3.9"}); msg != "" {
		t.Errorf("3.10.2 satisfies >=3.9, got %q", msg)
	}
	if msg := CheckBin(BinRequirement{Name: "fakebin", MinVersion: "3.11"}); !strings.Contains(msg, "3.10.2") {
		t.Errorf("3.10.2 does not satisfy >=3.11, got %q", msg)
	}
	if msg := CheckBin(BinRequirement{Name: "nover", MinVersion: "1"}); !strings.Contains(msg, "couldn't be read") {
		t.Errorf("unreadable version: %q", msg)
	}
	if msg := CheckBin(BinRequirement{Name: "broken", MinVersion: "1"}); !strings.Contains(msg, "failed") {
		t.Errorf("failing --version: %q", msg)
	}
}

func TestRefString(t *testing.T) {
	if s := (Ref{KindMCP, "team-a", "jira"}).String(); s != "mcp:team-a/jira" {
		t.Errorf("String() = %q", s)
	}
}
