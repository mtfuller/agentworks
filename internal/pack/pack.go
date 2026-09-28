// Package pack creates and installs deterministic, platform-independent
// archives containing a resolved format-2 agent team.
package pack

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mtfuller/agentworks/internal/resolver"
	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/targets/filecopy"
	"gopkg.in/yaml.v3"
)

const Format = 1

var archiveTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
}

type Manifest struct {
	Format        int               `json:"format"`
	Project       string            `json:"project"`
	Team          string            `json:"team"`
	PlanDigest    string            `json:"plan_digest"`
	ContentDigest string            `json:"content_digest"`
	Plan          resolver.TeamPlan `json:"plan"`
	Files         []File            `json:"files"`
}

type Result struct {
	Path          string            `json:"path"`
	ArchiveDigest string            `json:"archive_digest"`
	ContentDigest string            `json:"content_digest"`
	Files         int               `json:"files"`
	Plan          resolver.TeamPlan `json:"plan"`
}

type entry struct {
	path string
	data []byte
	mode fs.FileMode
}

// Create resolves teamName, vendors its complete closure, and writes a
// byte-for-byte deterministic archive to output.
func Create(root, teamName, output string) (Result, error) {
	plan, err := resolver.ResolveTeam(root, teamName, resolver.Options{AvailableProviders: []spec.Provider{
		spec.ProviderHost, spec.ProviderContainer, spec.ProviderRemote,
	}})
	if err != nil {
		return Result{}, err
	}
	entries, err := closure(root, plan)
	if err != nil {
		return Result{}, err
	}
	manifest := Manifest{Format: Format, Project: plan.Project, Team: plan.Team, PlanDigest: plan.Digest, Plan: *plan, Files: make([]File, 0, len(entries))}
	for _, item := range entries {
		digest := sha256.Sum256(item.data)
		manifest.Files = append(manifest.Files, File{Path: item.path, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(item.data)), Mode: uint32(packMode(item.mode))})
	}
	manifest.ContentDigest, err = contentDigest(manifest.Files)
	if err != nil {
		return Result{}, err
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encode pack manifest: %w", err)
	}
	manifestData = append(manifestData, '\n')
	entries = append(entries, entry{path: "pack.json", data: manifestData, mode: 0o644})
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })

	if output == "" {
		output = filepath.Join(root, "dist", "packs", fmt.Sprintf("%s-%s-%s.agentworks", plan.Project, strings.ReplaceAll(plan.Team, "/", "-"), plan.Digest[:12]))
	}
	if err := writeArchive(output, entries); err != nil {
		return Result{}, err
	}
	digest, err := hashFile(output)
	if err != nil {
		return Result{}, err
	}
	return Result{Path: output, ArchiveDigest: digest, ContentDigest: manifest.ContentDigest, Files: len(manifest.Files), Plan: *plan}, nil
}

func closure(root string, plan *resolver.TeamPlan) ([]entry, error) {
	project, err := spec.LoadProject(root)
	if err != nil {
		return nil, err
	}
	packedProject := *project
	packedProject.Teams = []string{plan.Team}
	packedProject.Dependencies = spec.Dependencies{Skills: map[string]spec.Dependency{}, Tools: map[string]spec.Dependency{}}
	projectData, err := yaml.Marshal(packedProject)
	if err != nil {
		return nil, err
	}
	byPath := map[string]entry{"agentworks.yaml": {path: "agentworks.yaml", data: projectData, mode: 0o644}}
	addDir := func(source, destination string) error {
		return walkFiles(source, func(relative string, data []byte, mode fs.FileMode) error {
			name := filepath.ToSlash(filepath.Join(destination, relative))
			if _, exists := byPath[name]; exists {
				return fmt.Errorf("pack path %q occurs more than once", name)
			}
			byPath[name] = entry{path: name, data: data, mode: mode}
			return nil
		})
	}
	if err := addDir(filepath.Join(root, "teams", filepath.FromSlash(plan.Team)), filepath.Join("teams", filepath.FromSlash(plan.Team))); err != nil {
		return nil, err
	}
	for _, agent := range plan.Agents {
		if err := addDir(filepath.Join(root, "agents", filepath.FromSlash(agent.Name)), filepath.Join("agents", filepath.FromSlash(agent.Name))); err != nil {
			return nil, err
		}
	}
	for _, skill := range plan.Skills {
		source := componentPath(root, "skills", skill.Name, skill.Origin)
		if err := addDir(source, filepath.Join("skills", filepath.FromSlash(skill.Name))); err != nil {
			return nil, err
		}
	}
	for _, tool := range plan.Tools {
		source := componentPath(root, "tools", tool.Name, tool.Origin)
		if err := addDir(source, filepath.Join("tools", filepath.FromSlash(tool.Name))); err != nil {
			return nil, err
		}
	}
	memory := map[string]bool{}
	if plan.Memory != "" {
		memory[plan.Memory] = true
	}
	for _, agent := range plan.Agents {
		if agent.Memory != "" {
			memory[agent.Memory] = true
		}
	}
	for name := range memory {
		if err := addSingleFile(root, name, byPath); err != nil {
			return nil, err
		}
	}

	routes, routeIssues := spec.DiscoverRoutes(root)
	if len(routeIssues) > 0 {
		return nil, fmt.Errorf("discover pack routes: %s", routeIssues[0].Error)
	}
	sourceNames := map[string]bool{}
	for _, route := range routes {
		if route.Invoke.Team != plan.Team {
			continue
		}
		if err := addDir(filepath.Join(root, "routes", filepath.FromSlash(route.Name)), filepath.Join("routes", filepath.FromSlash(route.Name))); err != nil {
			return nil, err
		}
		if route.When.Source != "" {
			sourceNames[route.When.Source] = true
		}
	}
	for name := range sourceNames {
		if _, err := spec.LoadSource(root, name); err != nil {
			return nil, fmt.Errorf("route source %q: %w", name, err)
		}
		if err := addDir(filepath.Join(root, "sources", filepath.FromSlash(name)), filepath.Join("sources", filepath.FromSlash(name))); err != nil {
			return nil, err
		}
	}

	result := make([]entry, 0, len(byPath))
	for _, item := range byPath {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].path < result[j].path })
	return result, nil
}

func componentPath(root, kind, name string, origin resolver.Origin) string {
	if origin == resolver.OriginInstalled {
		return filepath.Join(root, ".agentworks", "deps", kind, filepath.FromSlash(name))
	}
	return filepath.Join(root, kind, filepath.FromSlash(name))
}

func addSingleFile(root, relative string, entries map[string]entry) error {
	name := filepath.ToSlash(filepath.Clean(relative))
	if name == "." || strings.HasPrefix(name, "../") || filepath.IsAbs(relative) {
		return fmt.Errorf("pack file %q escapes the project", relative)
	}
	path := filepath.Join(root, filepath.FromSlash(name))
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("read pack file %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("pack file %s is not a regular file", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	entries[name] = entry{path: name, data: data, mode: info.Mode()}
	return nil
}

var skippedDirs = map[string]bool{".git": true, ".venv": true, "venv": true, "node_modules": true, "__pycache__": true}

func walkFiles(root string, visit func(string, []byte, fs.FileMode) error) error {
	return filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if item.IsDir() {
			if skippedDirs[item.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("pack input %s is not a regular file", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return visit(relative, data, info.Mode())
	})
}

func writeArchive(path string, entries []entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".agentworks-pack-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	writer := zip.NewWriter(temporary)
	for _, item := range entries {
		header := &zip.FileHeader{Name: item.path, Method: zip.Store, Modified: archiveTime}
		header.SetMode(packMode(item.mode))
		output, err := writer.CreateHeader(header)
		if err != nil {
			writer.Close()
			temporary.Close()
			return err
		}
		if _, err := output.Write(item.data); err != nil {
			writer.Close()
			temporary.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("install pack archive: %w", err)
	}
	return nil
}

func packMode(mode fs.FileMode) fs.FileMode {
	if mode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func contentDigest(files []File) (string, error) {
	data, err := json.Marshal(files)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Install verifies archive before atomically moving its contents into an
// absent destination directory.
func Install(archivePath, destination string) (Manifest, error) {
	if _, err := os.Stat(destination); err == nil {
		return Manifest{}, fmt.Errorf("pack destination already exists: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, err
	}
	parent := filepath.Dir(filepath.Clean(destination))
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return Manifest{}, err
	}
	stage, err := os.MkdirTemp(parent, ".agentworks-install-*")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(stage)
	if err := filecopy.Unzip(archivePath, stage); err != nil {
		return Manifest{}, err
	}
	manifest, err := Verify(stage)
	if err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(stage, destination); err != nil {
		return Manifest{}, fmt.Errorf("install pack: %w", err)
	}
	return manifest, nil
}

// Verify validates a previously extracted pack directory without executing it.
func Verify(root string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "pack.json"))
	if err != nil {
		return Manifest{}, fmt.Errorf("read pack manifest: %w", err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode pack manifest: %w", err)
	}
	if manifest.Format != Format || manifest.Project == "" || manifest.Team == "" || manifest.PlanDigest == "" {
		return Manifest{}, errors.New("pack manifest is invalid")
	}
	digest, err := contentDigest(manifest.Files)
	if err != nil || digest != manifest.ContentDigest {
		return Manifest{}, errors.New("pack content manifest digest does not match")
	}
	expected := map[string]File{}
	for _, file := range manifest.Files {
		if file.Path == "pack.json" || file.Path == "" || strings.HasPrefix(file.Path, "/") || strings.HasPrefix(file.Path, "../") || expected[file.Path].Path != "" {
			return Manifest{}, fmt.Errorf("invalid pack entry %q", file.Path)
		}
		expected[file.Path] = file
	}
	seen := map[string]bool{}
	err = filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if item.IsDir() {
			return nil
		}
		info, err := item.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("pack entry %q is not a regular file", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if name == "pack.json" {
			return nil
		}
		record, ok := expected[name]
		if !ok {
			return fmt.Errorf("unlisted pack entry %q", name)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		actual := sha256.Sum256(content)
		if hex.EncodeToString(actual[:]) != record.SHA256 || int64(len(content)) != record.Size {
			return fmt.Errorf("pack entry %q failed integrity verification", name)
		}
		seen[name] = true
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	if len(seen) != len(expected) {
		return Manifest{}, errors.New("pack is missing one or more declared files")
	}
	return manifest, nil
}
