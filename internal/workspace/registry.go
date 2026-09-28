// Package workspace owns runtime-only workspace bindings and portable change summaries.
package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const AdHocPrefix = "adhoc:"

type Binding struct {
	Alias     string
	Path      string
	Confirmed bool
}

// Registry holds ad hoc bindings only for the lifetime of one Studio process.
// Absolute paths never enter portable definitions, events, or run records.
type Registry struct {
	mu       sync.RWMutex
	bindings map[string]Binding
}

func NewRegistry() *Registry { return &Registry{bindings: map[string]Binding{}} }

// Bind validates and canonicalizes a directory. confirmed records that the user
// explicitly approved write-capable use for this Studio session.
func (registry *Registry) Bind(path string, confirmed bool) (Binding, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Binding{}, errors.New("workspace path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Binding{}, fmt.Errorf("resolve workspace path: %w", err)
	}
	if canonical, evalErr := filepath.EvalSymlinks(absolute); evalErr == nil {
		absolute = canonical
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return Binding{}, fmt.Errorf("inspect workspace path: %w", err)
	}
	if !info.IsDir() {
		return Binding{}, errors.New("workspace path is not a directory")
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for alias, binding := range registry.bindings {
		if samePath(binding.Path, absolute) {
			if confirmed && !binding.Confirmed {
				binding.Confirmed = true
				registry.bindings[alias] = binding
			}
			return binding, nil
		}
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return Binding{}, fmt.Errorf("create workspace binding: %w", err)
	}
	binding := Binding{Alias: AdHocPrefix + hex.EncodeToString(token), Path: absolute, Confirmed: confirmed}
	registry.bindings[binding.Alias] = binding
	return binding, nil
}

func (registry *Registry) Resolve(alias string) (Binding, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	binding, ok := registry.bindings[alias]
	return binding, ok
}

func IsAdHoc(alias string) bool { return strings.HasPrefix(alias, AdHocPrefix) }

func samePath(left, right string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}
