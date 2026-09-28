package releasegate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const approvedEvidence = `status: approved
tested_commit: 0123456789abcdef0123456789abcdef01234567
soak:
  started_at: 2026-09-25T12:00:00Z
  completed_at: 2026-09-27T12:00:00Z
  evidence: docs/operations/soak-results.md
software_factory_dogfood:
  jira_issue: AW-123
  pull_request: https://github.com/example/repo/pull/1
  findings: docs/operations/dogfood.md
authenticated_conformance:
  workflow_run: https://github.com/example/repo/actions/runs/1
security_review:
  report: docs/operations/security-review.md
  open_critical: 0
  open_high: 0
approval:
  reviewer: Release Reviewer
  approved_at: 2026-09-27T13:00:00Z
`

func TestLoadApprovedEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.yaml")
	if err := os.WriteFile(path, []byte(approvedEvidence), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsIncompleteOrUnknownEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.yaml")
	invalid := strings.Replace(approvedEvidence, "status: approved", "status: pending\nunknown: true", 1)
	if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "field unknown") {
		t.Fatalf("error=%v", err)
	}
}

func TestValidateRejectsShortSoakAndFindings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.yaml")
	invalid := strings.Replace(approvedEvidence, "2026-09-27T12:00:00Z", "2026-09-26T12:00:00Z", 1)
	invalid = strings.Replace(invalid, "open_high: 0", "open_high: 1", 1)
	if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "48 hours") || !strings.Contains(err.Error(), "security findings") {
		t.Fatalf("error=%v", err)
	}
}

func TestValidateForCommit(t *testing.T) {
	evidence, err := Load(writeEvidence(t, approvedEvidence))
	if err != nil {
		t.Fatal(err)
	}
	commit := "0123456789abcdef0123456789abcdef01234567"
	if err := evidence.ValidateForCommit("  " + commit + "  "); err != nil {
		t.Fatalf("matching commit: %v", err)
	}
	if err := evidence.ValidateForCommit("not-a-sha"); err == nil || !strings.Contains(err.Error(), "expected release commit") {
		t.Fatalf("invalid expected commit: %v", err)
	}
	if err := evidence.ValidateForCommit("fedcba9876543210fedcba9876543210fedcba98"); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched commit: %v", err)
	}
	evidence.Status = "pending"
	if err := evidence.ValidateForCommit(commit); err == nil || !strings.Contains(err.Error(), "status must be approved") {
		t.Fatalf("invalid evidence: %v", err)
	}
}

func TestLoadRejectsMissingAndInvalidApprovalTimestamp(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("Load missing evidence succeeded")
	}
	invalid := strings.Replace(approvedEvidence, "approved_at: 2026-09-27T13:00:00Z", "approved_at: not-a-time", 1)
	_, err := Load(writeEvidence(t, invalid))
	if err == nil || !strings.Contains(err.Error(), "approval.approved_at") {
		t.Fatalf("invalid approval timestamp: %v", err)
	}
}

func writeEvidence(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "evidence.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
