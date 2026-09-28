// Package releasegate validates the external evidence required before the
// runtime release workflow may publish a tag.
package releasegate

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Evidence struct {
	Status       string `yaml:"status"`
	TestedCommit string `yaml:"tested_commit"`
	Soak         struct {
		StartedAt   string `yaml:"started_at"`
		CompletedAt string `yaml:"completed_at"`
		Evidence    string `yaml:"evidence"`
	} `yaml:"soak"`
	SoftwareFactoryDogfood struct {
		JiraIssue   string `yaml:"jira_issue"`
		PullRequest string `yaml:"pull_request"`
		Findings    string `yaml:"findings"`
	} `yaml:"software_factory_dogfood"`
	AuthenticatedConformance struct {
		WorkflowRun string `yaml:"workflow_run"`
	} `yaml:"authenticated_conformance"`
	SecurityReview struct {
		Report       string `yaml:"report"`
		OpenCritical int    `yaml:"open_critical"`
		OpenHigh     int    `yaml:"open_high"`
	} `yaml:"security_review"`
	Approval struct {
		Reviewer   string `yaml:"reviewer"`
		ApprovedAt string `yaml:"approved_at"`
	} `yaml:"approval"`
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func Load(path string) (Evidence, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Evidence{}, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var evidence Evidence
	if err := decoder.Decode(&evidence); err != nil {
		return Evidence{}, fmt.Errorf("parse release evidence: %w", err)
	}
	if err := evidence.Validate(); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

// ValidateForCommit verifies that approved evidence was collected for the
// exact source commit being released. A structurally valid evidence document
// for another commit must never authorize a tag by accident.
func (evidence Evidence) ValidateForCommit(commit string) error {
	if err := evidence.Validate(); err != nil {
		return err
	}
	commit = strings.TrimSpace(commit)
	if !commitPattern.MatchString(commit) {
		return errors.New("expected release commit must be a full lowercase Git commit SHA")
	}
	if strings.TrimSpace(evidence.TestedCommit) != commit {
		return fmt.Errorf("tested_commit %s does not match release commit %s", evidence.TestedCommit, commit)
	}
	return nil
}

func (evidence Evidence) Validate() error {
	var problems []error
	if evidence.Status != "approved" {
		problems = append(problems, errors.New("status must be approved"))
	}
	if !commitPattern.MatchString(strings.TrimSpace(evidence.TestedCommit)) {
		problems = append(problems, errors.New("tested_commit must be a full lowercase Git commit SHA"))
	}
	started, startErr := time.Parse(time.RFC3339, evidence.Soak.StartedAt)
	completed, completeErr := time.Parse(time.RFC3339, evidence.Soak.CompletedAt)
	if startErr != nil || completeErr != nil {
		problems = append(problems, errors.New("soak start and completion must be RFC3339 timestamps"))
	} else if completed.Sub(started) < 48*time.Hour {
		problems = append(problems, errors.New("soak must span at least 48 hours"))
	}
	require := func(field, value string) {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, fmt.Errorf("%s is required", field))
		}
	}
	require("soak.evidence", evidence.Soak.Evidence)
	require("software_factory_dogfood.jira_issue", evidence.SoftwareFactoryDogfood.JiraIssue)
	require("software_factory_dogfood.pull_request", evidence.SoftwareFactoryDogfood.PullRequest)
	require("software_factory_dogfood.findings", evidence.SoftwareFactoryDogfood.Findings)
	require("authenticated_conformance.workflow_run", evidence.AuthenticatedConformance.WorkflowRun)
	require("security_review.report", evidence.SecurityReview.Report)
	require("approval.reviewer", evidence.Approval.Reviewer)
	require("approval.approved_at", evidence.Approval.ApprovedAt)
	if evidence.SecurityReview.OpenCritical != 0 || evidence.SecurityReview.OpenHigh != 0 {
		problems = append(problems, errors.New("critical and high security findings must both be zero"))
	}
	if evidence.Approval.ApprovedAt != "" {
		approved, err := time.Parse(time.RFC3339, evidence.Approval.ApprovedAt)
		if err != nil {
			problems = append(problems, errors.New("approval.approved_at must be an RFC3339 timestamp"))
		} else if completeErr == nil && approved.Before(completed) {
			problems = append(problems, errors.New("release approval cannot predate soak completion"))
		}
	}
	return errors.Join(problems...)
}
