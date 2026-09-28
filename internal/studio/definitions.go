package studio

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/mtfuller/agentworks/internal/spec"
	"github.com/mtfuller/agentworks/internal/targets/agentskills"
)

const maxDefinitionBytes = 1 << 20

type definitionFile struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type definitionDocument struct {
	definitionFile
	Content string `json:"content"`
}

type definitionEditor struct {
	root string
	mu   sync.Mutex
}

func (editor *definitionEditor) list(writer http.ResponseWriter, request *http.Request) {
	if strings.TrimSpace(editor.root) == "" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"error": "project definitions are unavailable"})
		return
	}
	files, err := editor.files()
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]any{"error": "list project definitions"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"files": files})
}

func (editor *definitionEditor) get(writer http.ResponseWriter, request *http.Request) {
	if strings.TrimSpace(editor.root) == "" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"error": "project definitions are unavailable"})
		return
	}
	document, err := editor.read(request.PathValue("path"))
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(writer, http.StatusNotFound, map[string]any{"error": "definition not found"})
		return
	}
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]any{"error": "read project definition"})
		return
	}
	writeJSON(writer, http.StatusOK, document)
}

func (editor *definitionEditor) put(writer http.ResponseWriter, request *http.Request) {
	if strings.TrimSpace(editor.root) == "" {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]any{"error": "project definitions are unavailable"})
		return
	}
	// A JSON string may expand each source byte into a Unicode escape.
	request.Body = http.MaxBytesReader(writer, request.Body, 6*maxDefinitionBytes+4096)
	var input struct {
		ExpectedSHA256 string `json:"expected_sha256"`
		Content        string `json:"content"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"error": "invalid definition update"})
		return
	}
	if err := decodeSingleJSON(decoder); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if strings.TrimSpace(input.ExpectedSHA256) == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"error": "expected_sha256 is required"})
		return
	}
	if len(input.Content) > maxDefinitionBytes || !utf8.ValidString(input.Content) || strings.ContainsRune(input.Content, '\x00') {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]any{"error": "definition must be UTF-8 text no larger than 1 MiB"})
		return
	}

	editor.mu.Lock()
	defer editor.mu.Unlock()
	current, err := editor.read(request.PathValue("path"))
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(writer, http.StatusNotFound, map[string]any{"error": "definition not found"})
		return
	}
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]any{"error": "read project definition"})
		return
	}
	if current.SHA256 != input.ExpectedSHA256 {
		writeJSON(writer, http.StatusConflict, map[string]any{
			"error": "definition changed on disk; reload before saving", "current_sha256": current.SHA256,
		})
		return
	}
	if err := validateDefinition(current.Path, current.Kind, []byte(input.Content)); err != nil {
		writeJSON(writer, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	absolute := filepath.Join(editor.root, filepath.FromSlash(current.Path))
	info, err := os.Lstat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		writeJSON(writer, http.StatusConflict, map[string]any{"error": "definition changed on disk; reload before saving"})
		return
	}
	if err := replaceFile(absolute, []byte(input.Content), info.Mode().Perm()); err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]any{"error": "save project definition"})
		return
	}
	saved, err := editor.read(current.Path)
	if err != nil {
		writeJSON(writer, http.StatusInternalServerError, map[string]any{"error": "reload saved definition"})
		return
	}
	writeJSON(writer, http.StatusOK, saved)
}

func (editor *definitionEditor) files() ([]definitionFile, error) {
	files := []definitionFile{}
	err := filepath.WalkDir(editor.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == editor.root {
			return nil
		}
		relative, err := filepath.Rel(editor.root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 || relative == ".git" || relative == ".agentworks" {
				return filepath.SkipDir
			}
			return nil
		}
		kind := definitionKind(relative)
		if kind == "" || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		document, err := editor.read(relative)
		if err != nil {
			return err
		}
		files = append(files, document.definitionFile)
		return nil
	})
	return files, err
}

func (editor *definitionEditor) read(relative string) (definitionDocument, error) {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if clean != relative || definitionKind(clean) == "" || strings.Contains(relative, `\`) {
		return definitionDocument{}, os.ErrNotExist
	}
	absolute := filepath.Join(editor.root, filepath.FromSlash(clean))
	info, err := os.Lstat(absolute)
	if err != nil {
		return definitionDocument{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxDefinitionBytes {
		return definitionDocument{}, os.ErrNotExist
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return definitionDocument{}, err
	}
	if !utf8.Valid(content) || strings.IndexByte(string(content), 0) >= 0 {
		return definitionDocument{}, fmt.Errorf("definition is not UTF-8 text")
	}
	digest := sha256.Sum256(content)
	return definitionDocument{
		definitionFile: definitionFile{Path: clean, Kind: definitionKind(clean), SHA256: hex.EncodeToString(digest[:]), Size: int64(len(content))},
		Content:        string(content),
	}, nil
}

func definitionKind(relative string) string {
	if relative == spec.ProjectFile {
		return "project"
	}
	parts := strings.Split(relative, "/")
	if len(parts) >= 2 && parts[0] == "memory" && strings.HasSuffix(strings.ToLower(parts[len(parts)-1]), ".md") {
		return "memory"
	}
	if len(parts) < 3 {
		return ""
	}
	for _, part := range parts[1 : len(parts)-1] {
		if part == "" || part == "." || part == ".." {
			return ""
		}
	}
	switch {
	case parts[0] == "teams" && parts[len(parts)-1] == spec.TeamFile:
		return "team"
	case parts[0] == "agents" && parts[len(parts)-1] == spec.AgentFile:
		return "agent"
	case parts[0] == "skills" && parts[len(parts)-1] == "SKILL.md":
		return "skill"
	case parts[0] == "tools" && parts[len(parts)-1] == spec.ToolFile:
		return "tool"
	case parts[0] == "sources" && parts[len(parts)-1] == spec.SourceFile:
		return "source"
	case parts[0] == "routes" && parts[len(parts)-1] == spec.RouteFile:
		return "route"
	default:
		return ""
	}
}

func validateDefinition(relative, kind string, content []byte) error {
	temporary, err := os.MkdirTemp("", "agentworks-definition-")
	if err != nil {
		return fmt.Errorf("prepare definition validation: %w", err)
	}
	defer os.RemoveAll(temporary)
	target := filepath.Join(temporary, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("prepare definition validation: %w", err)
	}
	if err := os.WriteFile(target, content, 0o600); err != nil {
		return fmt.Errorf("prepare definition validation: %w", err)
	}
	ref := strings.TrimSuffix(strings.TrimPrefix(relative, strings.Split(relative, "/")[0]+"/"), "/"+filepath.Base(relative))
	switch kind {
	case "project":
		_, err = spec.LoadProject(temporary)
	case "team":
		_, err = spec.LoadTeam(temporary, ref)
	case "agent":
		_, err = spec.LoadAgent(temporary, ref)
	case "tool":
		_, err = spec.LoadTool(temporary, ref)
	case "source":
		_, err = spec.LoadSource(temporary, ref)
	case "route":
		_, err = spec.LoadRoute(temporary, ref)
	case "skill":
		var skillName string
		artifact, readErr := agentskills.Read(filepath.Dir(target))
		if readErr != nil {
			err = readErr
		} else if validateErr := artifact.Validate(); validateErr != nil {
			err = validateErr
		} else {
			skillName = artifact.Name
			if skillName != filepath.Base(filepath.Dir(target)) {
				err = fmt.Errorf("SKILL.md name %q does not match directory %q", skillName, filepath.Base(filepath.Dir(target)))
			}
		}
	case "memory":
		return nil
	default:
		return errors.New("unsupported project definition")
	}
	if err != nil {
		return fmt.Errorf("definition is invalid: %w", err)
	}
	return nil
}

func decodeSingleJSON(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON document")
	}
	return nil
}
