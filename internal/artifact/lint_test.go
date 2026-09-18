package artifact

import (
	"strings"
	"testing"
)

func TestLintDescriptionEmptyReturnsNothing(t *testing.T) {
	a := &Artifact{Frontmatter: Frontmatter{Name: "demo", Description: ""}, Dir: "skills/demo"}
	if got := a.LintDescription(); got != nil {
		t.Errorf("LintDescription() = %v, want nil (Validate() owns the empty case)", got)
	}
}

func TestLintDescriptionTooLong(t *testing.T) {
	a := &Artifact{Frontmatter: Frontmatter{
		Name:        "demo",
		Description: strings.Repeat("word ", 210), // well over 1024 chars
	}, Dir: "skills/demo"}

	warnings := a.LintDescription()
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "limit") {
		t.Fatalf("LintDescription() = %v, want a single over-limit warning", warnings)
	}
}

func TestLintDescriptionTooShort(t *testing.T) {
	a := &Artifact{Frontmatter: Frontmatter{
		Name:        "pdf-helper",
		Description: "Helps with PDFs.", // the spec's own "poor example"
	}, Dir: "skills/pdf-helper"}

	warnings := a.LintDescription()
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "short") {
		t.Fatalf("LintDescription() = %v, want a single too-short warning", warnings)
	}
}

func TestLintDescriptionRestatesName(t *testing.T) {
	a := &Artifact{Frontmatter: Frontmatter{
		Name:        "csv-analyzer",
		Description: "CSV analyzer, a CSV analyzer, the CSV analyzer.",
	}, Dir: "skills/csv-analyzer"}

	warnings := a.LintDescription()
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "adds no information") {
		t.Fatalf("LintDescription() = %v, want a single redundant-with-name warning", warnings)
	}
}

func TestLintDescriptionGoodDescriptionHasNoWarnings(t *testing.T) {
	a := &Artifact{Frontmatter: Frontmatter{
		Name:        "pdf-processing",
		Description: "Extracts text and tables from PDF files, fills PDF forms, and merges multiple PDFs. Use when working with PDF documents or when the user mentions PDFs, forms, or document extraction.",
	}, Dir: "skills/pdf-processing"}

	if got := a.LintDescription(); len(got) != 0 {
		t.Errorf("LintDescription() = %v, want no warnings for a spec-quality description", got)
	}
}

func TestLintOverlapFlagsSameKindDuplicates(t *testing.T) {
	artifacts := []*Artifact{
		{Frontmatter: Frontmatter{Kind: KindSkill, Name: "csv-outliers", Description: "Analyze a CSV file and flag rows that stand out from the rest."}, Dir: "skills/csv-outliers"},
		{Frontmatter: Frontmatter{Kind: KindSkill, Name: "csv-anomalies", Description: "Analyze a CSV file and flag rows that stand out from the rest of the data."}, Dir: "skills/csv-anomalies"},
	}

	warnings := LintOverlap(artifacts)
	if len(warnings) != 2 {
		t.Fatalf("LintOverlap() returned %d warnings, want 2 (one per overlapping artifact)", len(warnings))
	}
	dirs := map[string]bool{warnings[0].Dir: true, warnings[1].Dir: true}
	if !dirs["skills/csv-outliers"] || !dirs["skills/csv-anomalies"] {
		t.Errorf("LintOverlap() warnings = %v, want one attributed to each artifact", warnings)
	}
}

func TestLintOverlapIgnoresDifferentKinds(t *testing.T) {
	artifacts := []*Artifact{
		{Frontmatter: Frontmatter{Kind: KindSkill, Name: "csv-outliers", Description: "Analyze a CSV file and flag rows that stand out from the rest."}, Dir: "skills/csv-outliers"},
		{Frontmatter: Frontmatter{Kind: KindMCP, Name: "csv-anomalies", Description: "Analyze a CSV file and flag rows that stand out from the rest."}, Dir: "tools/csv-anomalies"},
	}

	if got := LintOverlap(artifacts); len(got) != 0 {
		t.Errorf("LintOverlap() = %v, want no warnings across different kinds", got)
	}
}

func TestLintOverlapIgnoresDistinctDescriptions(t *testing.T) {
	artifacts := []*Artifact{
		{Frontmatter: Frontmatter{Kind: KindSkill, Name: "csv-outliers", Description: "Analyze a CSV file and flag rows that stand out from the rest."}, Dir: "skills/csv-outliers"},
		{Frontmatter: Frontmatter{Kind: KindSkill, Name: "pdf-processing", Description: "Extract text and tables from PDF files and fill PDF forms."}, Dir: "skills/pdf-processing"},
	}

	if got := LintOverlap(artifacts); len(got) != 0 {
		t.Errorf("LintOverlap() = %v, want no warnings for distinct descriptions", got)
	}
}
