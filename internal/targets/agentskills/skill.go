// Package agentskills writes a skill directory in the shape defined by the
// Agent Skills specification (https://agentskills.io/specification): a
// SKILL.md with YAML frontmatter + Markdown body, plus whatever supporting
// files (scripts/, references/, assets/, ...) the skill needs.
//
// This is the one place that knows that format. It's shared by every
// export target that consumes it as-is (Claude Code, ChatGPT) or wraps it
// in a container of its own (GitHub Copilot's Agent Plugins).
package agentskills

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// Frontmatter is the agentskills.io SKILL.md frontmatter. It's deliberately
// narrower than artifact.Frontmatter, so writing a skill drops
// AgentWorks-only fields (version, targets, entrypoint, test, ...) rather
// than leaking them into the vendor file.
type Frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// License and Compatibility are optional per the spec and left unset
	// unless an artifact's frontmatter Extra carries them (see
	// frontmatterFor).
	License       string `yaml:"license,omitempty"`
	Compatibility string `yaml:"compatibility,omitempty"`
}

// Write writes a spec-compliant skill directory at destDir: SKILL.md plus
// every supporting file from the artifact's own directory (excluding its
// AgentWorks <kind>.md manifest, which SKILL.md replaces). destDir is
// cleared first if it already exists.
func Write(a *artifact.Artifact, destDir string) error {
	if a.Kind != artifact.KindSkill {
		return fmt.Errorf("agentskills export only supports skills (got %s)", a.Kind)
	}
	if err := a.Validate(); err != nil {
		return fmt.Errorf("refusing to export invalid artifact: %w", err)
	}

	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("clearing %s: %w", destDir, err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", destDir, err)
	}

	if err := writeSkillMD(destDir, a); err != nil {
		return err
	}
	return copySupportingFiles(a, destDir)
}

func writeSkillMD(destDir string, a *artifact.Artifact) error {
	fm := frontmatterFor(a)
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

// frontmatterFor builds the vendor SKILL.md frontmatter from an artifact,
// carrying over the optional license/compatibility fields only if the
// artifact's own frontmatter declared them.
func frontmatterFor(a *artifact.Artifact) Frontmatter {
	return Frontmatter{
		Name:          a.Name,
		Description:   a.Description,
		License:       a.ExtraString("license"),
		Compatibility: a.ExtraString("compatibility"),
	}
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

// Zip packages srcDir as srcDir+".zip", with srcDir's base name as the
// single top-level folder inside the archive.
func Zip(srcDir string) (string, error) {
	zipPath := srcDir + ".zip"
	zf, err := os.Create(zipPath)
	if err != nil {
		return "", fmt.Errorf("creating %s: %w", zipPath, err)
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
		return "", fmt.Errorf("zipping %s: %w", srcDir, err)
	}
	return zipPath, nil
}
