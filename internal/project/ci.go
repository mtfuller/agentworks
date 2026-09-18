package project

import (
	"fmt"
	"os"
	"path/filepath"
)

// ciWorkflowPath is where WriteCIWorkflow puts the workflow, relative to the
// project root.
var ciWorkflowPath = filepath.Join(".github", "workflows", "agentworks.yml")

const ciWorkflow = `name: AgentWorks

on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      # Pin this to a release tag (e.g. mtfuller/agentworks@v0.3.0) once you
      # depend on it; @main tracks the latest.
      - uses: mtfuller/agentworks@main
        with:
          # Available checks: validate, doctor, marketplace (only when a
          # marketplace.json is committed), test, eval, status.
          checks: validate,doctor,marketplace
          # Fail on weak descriptions and risky shell commands, not just errors.
          strict: "true"
`

// WriteCIWorkflow writes a GitHub Actions workflow running the project's
// AgentWorks checks into dir/.github/workflows/agentworks.yml and returns its
// path. It refuses to overwrite an existing workflow: this is scaffolding,
// and a hand-edited pipeline is worth more than a fresh template.
func WriteCIWorkflow(dir string) (string, error) {
	path := filepath.Join(dir, ciWorkflowPath)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(ciWorkflow), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}
