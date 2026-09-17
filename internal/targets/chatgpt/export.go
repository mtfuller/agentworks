// Package chatgpt implements the "chatgpt" export target: ChatGPT consumes
// the same Agent Skills format Claude Code does (see
// internal/targets/agentskills) -- upload it as a directory, or as a .zip
// containing that one directory.
package chatgpt

import (
	"fmt"
	"path/filepath"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
	"github.com/mtfuller/agentworks/internal/targets/agentskills"
)

func init() {
	targets.Register(skillExporter{})
}

const TargetID = "chatgpt"

type skillExporter struct{}

func (skillExporter) TargetID() string { return TargetID }

func (skillExporter) Export(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	if a.Kind != artifact.KindSkill {
		return "", fmt.Errorf("chatgpt export only supports skills right now (got %s)", a.Kind)
	}

	destDir := filepath.Join(outDir, a.Name)
	if err := agentskills.Write(a, destDir); err != nil {
		return "", err
	}
	if opts.Zip {
		return agentskills.Zip(destDir)
	}
	return destDir, nil
}
