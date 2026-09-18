package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
)

// jsonSchemaVersion is bumped only for a breaking change to a --json
// document's shape (a removed or retyped field). Adding fields is not
// breaking, so consumers should ignore keys they don't know.
const jsonSchemaVersion = 1

var (
	jsonFlag bool
	// jsonEmitted records that a JSON document has already been written, so
	// Execute doesn't add a second (error) document after a command that
	// reported its own failure in-band.
	jsonEmitted bool
)

// envelope is the header every --json document starts with. Commands embed
// it so the fields stay flat at the top level.
type envelope struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	// OK is true when the command succeeded (would exit 0).
	OK bool `json:"ok"`
}

func newEnvelope(command string, ok bool) envelope {
	return envelope{SchemaVersion: jsonSchemaVersion, Command: command, OK: ok}
}

// emitJSON writes doc as one indented JSON document on stdout. In --json
// mode all human-readable messages go to stderr, so this is the only thing
// on stdout.
func emitJSON(doc any) error {
	jsonEmitted = true
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// errorDoc is what --json prints when a command fails before it could build
// its own document (e.g. not inside a project).
type errorDoc struct {
	envelope
	Error string `json:"error"`
}

// reportError prints err the way Execute always has, plus -- in --json mode,
// when no document was written -- an errorDoc on stdout so a consumer gets
// parseable output for every outcome.
func reportError(cmdName string, err error) {
	color.Error("Error: %v", err)
	if jsonFlag && !jsonEmitted {
		_ = emitJSON(errorDoc{envelope: newEnvelope(cmdName, false), Error: err.Error()})
	}
}

// itemPath returns a's directory relative to the project root in slash form,
// so JSON output is stable across machines and CI checkouts. Falls back to
// the path as-is when it isn't under the root (or there is no project).
func itemPath(a *artifact.Artifact) string {
	root, err := projectRoot()
	if err != nil {
		return filepath.ToSlash(a.Dir)
	}
	absDir, err := filepath.Abs(a.Dir)
	if err != nil {
		return filepath.ToSlash(a.Dir)
	}
	rel, err := filepath.Rel(root, absDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(a.Dir)
	}
	return filepath.ToSlash(rel)
}

// stdoutForChildren is where a spawned test/eval process's own stdout goes:
// normally ours, but stderr in --json mode so it can't corrupt the document.
func stdoutForChildren() *os.File {
	if jsonFlag {
		return os.Stderr
	}
	return os.Stdout
}

// quietly runs fn with the human message helpers silenced, for a step whose
// progress chatter would only obscure the caller's own report.
func quietly(fn func() error) error {
	prev := color.Output()
	color.SetOutput(io.Discard)
	defer color.SetOutput(prev)
	return fn()
}
