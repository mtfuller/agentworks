package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseCheckRejectsPendingRepositoryEvidence(t *testing.T) {
	path := filepath.Join("..", "docs", "operations", "m9-release-evidence.yaml")
	result := runCLIRaw(t, "release-check", path)
	if result.err == nil || !strings.Contains(result.err.Error(), "status must be approved") {
		t.Fatalf("result=%v output=%s", result.err, result.combined())
	}
}

func TestReleaseCheckRequiresMatchingCommitWhenRequested(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.yaml")
	data := `status: approved
tested_commit: 0123456789abcdef0123456789abcdef01234567
soak:
  started_at: 2026-09-25T12:00:00Z
  completed_at: 2026-09-27T12:00:00Z
  evidence: soak.md
software_factory_dogfood:
  jira_issue: AW-123
  pull_request: https://example.test/pull/1
  findings: dogfood.md
authenticated_conformance:
  workflow_run: https://example.test/actions/1
security_review:
  report: security.md
  open_critical: 0
  open_high: 0
approval:
  reviewer: Reviewer
  approved_at: 2026-09-27T13:00:00Z
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	matching := "0123456789abcdef0123456789abcdef01234567"
	if result := runCLIRaw(t, "release-check", "--commit", matching, path); result.err != nil {
		t.Fatalf("matching commit: %v\n%s", result.err, result.combined())
	}
	other := "fedcba9876543210fedcba9876543210fedcba98"
	result := runCLIRaw(t, "release-check", "--commit", other, path)
	if result.err == nil || !strings.Contains(result.err.Error(), "does not match release commit") {
		t.Fatalf("mismatched commit result=%v output=%s", result.err, result.combined())
	}
}

func TestReleaseCheckAcceptsCompleteEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.yaml")
	data := `status: approved
tested_commit: 0123456789abcdef0123456789abcdef01234567
soak:
  started_at: 2026-09-25T12:00:00Z
  completed_at: 2026-09-27T12:00:00Z
  evidence: soak.md
software_factory_dogfood:
  jira_issue: AW-123
  pull_request: https://example.test/pull/1
  findings: dogfood.md
authenticated_conformance:
  workflow_run: https://example.test/actions/1
security_review:
  report: security.md
  open_critical: 0
  open_high: 0
approval:
  reviewer: Reviewer
  approved_at: 2026-09-27T13:00:00Z
`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runCLIRaw(t, "release-check", path)
	if result.err != nil || !strings.Contains(result.stdout, "approved") {
		t.Fatalf("result=%v output=%s", result.err, result.combined())
	}
}
