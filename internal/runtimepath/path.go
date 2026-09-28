// Package runtimepath resolves platform-native locations for AgentWorks runtime data.
package runtimepath

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DataRoot returns the operating system's persistent, per-user AgentWorks data directory.
func DataRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find user home directory: %w", err)
	}
	return dataRoot(runtime.GOOS, home, os.Getenv)
}

// ProjectDir returns a stable runtime directory derived from the canonical local
// project path. Nothing is written to the project checkout.
func ProjectDir(projectRoot string) (string, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return "", errors.New("project root is required")
	}
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	if evaluated, evalErr := filepath.EvalSymlinks(absRoot); evalErr == nil {
		absRoot = evaluated
	}
	root, err := DataRoot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(filepath.Clean(absRoot)))
	return filepath.Join(root, "projects", hex.EncodeToString(digest[:8])), nil
}

// DatabasePath returns the per-project SQLite database path.
func DatabasePath(projectRoot string) (string, error) {
	dir, err := ProjectDir(projectRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.db"), nil
}

// LogRoot returns the directory for append-only per-run output chunks.
func LogRoot(projectRoot string) (string, error) {
	dir, err := ProjectDir(projectRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "logs"), nil
}

func dataRoot(goos, home string, getenv func(string) string) (string, error) {
	if strings.TrimSpace(home) == "" {
		return "", errors.New("user home directory is empty")
	}
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "AgentWorks"), nil
	case "windows":
		if local := strings.TrimSpace(getenv("LOCALAPPDATA")); local != "" {
			return filepath.Join(local, "AgentWorks"), nil
		}
		return filepath.Join(home, "AppData", "Local", "AgentWorks"), nil
	default:
		if state := strings.TrimSpace(getenv("XDG_STATE_HOME")); state != "" {
			return filepath.Join(state, "agentworks"), nil
		}
		return filepath.Join(home, ".local", "state", "agentworks"), nil
	}
}
