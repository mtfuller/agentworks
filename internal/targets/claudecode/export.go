// Package claudecode implements the "claude-code" export target: it turns
// an AgentWorks skill into a real Claude Code skill directory (a SKILL.md
// with just the name/description Claude Code actually reads, plus its
// supporting files), optionally zipped for upload/sharing.
package claudecode

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/targets"
)

func init() {
	targets.Register(skillExporter{})
}

const TargetID = "claude-code"

// skillFrontmatter is Claude Code's actual SKILL.md frontmatter shape --
// deliberately narrower than artifact.Frontmatter, so exporting drops
// AgentWorks-only fields (version, targets, entrypoint, test, ...) rather
// than leaking them into the vendor file.
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

type skillExporter struct{}

func (skillExporter) TargetID() string { return TargetID }

func (skillExporter) Export(a *artifact.Artifact, outDir string, opts targets.ExportOptions) (string, error) {
	if a.Kind != artifact.KindSkill {
		return "", fmt.Errorf("claude-code export only supports skills right now (got %s)", a.Kind)
	}
	if err := a.Validate(); err != nil {
		return "", fmt.Errorf("refusing to export invalid artifact: %w", err)
	}

	destDir := filepath.Join(outDir, a.Name)
	if err := os.RemoveAll(destDir); err != nil {
		return "", fmt.Errorf("clearing %s: %w", destDir, err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", destDir, err)
	}

	if err := writeSkillMD(destDir, a); err != nil {
		return "", err
	}
	if err := copySupportingFiles(a, destDir); err != nil {
		return "", err
	}

	if opts.Zip {
		zipPath := destDir + ".zip"
		if err := zipDir(destDir, zipPath); err != nil {
			return "", err
		}
		return zipPath, nil
	}
	return destDir, nil
}

func writeSkillMD(destDir string, a *artifact.Artifact) error {
	fm := skillFrontmatter{Name: a.Name, Description: a.Description}
	data, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("encoding SKILL.md frontmatter: %w", err)
	}
	content := "---\n" + string(data) + "---\n\n" + a.Body
	if err := os.WriteFile(filepath.Join(destDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		return fmt.Errorf("writing SKILL.md: %w", err)
	}
	return nil
}

// copySupportingFiles copies everything in the artifact's directory except
// its AgentWorks manifest (skill.md), which was already translated into
// SKILL.md above.
func copySupportingFiles(a *artifact.Artifact, destDir string) error {
	skip := a.Kind.FileName()
	return filepath.WalkDir(a.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(a.Dir, path)
		if err != nil {
			return err
		}
		if rel == "." || rel == skip {
			return nil
		}

		target := filepath.Join(destDir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening %s: %w", src, err)
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dst), err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}
	return nil
}

func zipDir(srcDir, zipPath string) error {
	zf, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", zipPath, err)
	}
	defer zf.Close()

	zw := zip.NewWriter(zf)
	defer zw.Close()

	base := filepath.Base(srcDir)
	err = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		w, err := zw.Create(filepath.ToSlash(filepath.Join(base, rel)))
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
	if err != nil {
		return fmt.Errorf("zipping %s: %w", srcDir, err)
	}
	return nil
}
