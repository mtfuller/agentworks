package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

var doctorStrict bool

var doctorCmd = &cobra.Command{
	Use:   "doctor [path]",
	Short: "Check that an artifact's declared commands and files are actually runnable",
	Long: `A static, side-effect-free preflight check -- unlike "test"/"run", it never
shells out to anything the artifact declares, it just checks that it could.

For every declared shell command ("command:"/"test:"/"build:"/"eval_runner:"), resolves
its interpreter/binary against PATH. For a declared "entrypoint:", checks the file exists.
For a tool's "auth:" environment variables, checks they're set -- as a warning, not a
failure, since they're only needed to actually call the tool (see 'agentworks run'),
not to discover what it offers.

With no path, checks every artifact in the project. Missing interpreters/binaries and
missing entrypoint files always fail the command; missing "auth:" variables only do
with --strict.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var toCheck []*artifact.Artifact

		if len(args) == 1 {
			a, err := loadArtifactAtPath(args[0])
			if err != nil {
				return err
			}
			toCheck = append(toCheck, a)
		} else {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			found, errs := project.Discover(root)
			for _, e := range errs {
				color.Warning("%v", e)
			}
			toCheck = found
		}

		if len(toCheck) == 0 {
			if jsonFlag {
				return emitJSON(doctorDoc{envelope: newEnvelope("doctor", true), Strict: doctorStrict, Artifacts: []doctorItem{}})
			}
			color.Info("No artifacts to check.")
			return nil
		}

		env := os.Environ()
		failed, warned := 0, 0
		items := make([]doctorItem, 0, len(toCheck))
		for _, a := range toCheck {
			issues := doctorChecks(a, env)
			item := doctorItem{Kind: string(a.Kind), Name: a.Name, Path: itemPath(a), Issues: []doctorIssueDoc{}}
			if len(issues) == 0 {
				color.Success("%s (%s)", a.Name, a.Kind)
			}
			for _, issue := range issues {
				severity := "warning"
				if issue.fatal {
					severity = "error"
					color.Error("%s: %s", a.Dir, issue.message)
					failed++
				} else {
					color.Warning("%s: %s", a.Dir, issue.message)
					warned++
				}
				item.Issues = append(item.Issues, doctorIssueDoc{Severity: severity, Message: issue.message})
			}
			items = append(items, item)
		}

		if doctorStrict {
			failed += warned
		}
		var failErr error
		if failed > 0 {
			failErr = fmt.Errorf("%d issue(s) found", failed)
		}
		if jsonFlag {
			if err := emitJSON(doctorDoc{
				envelope:  newEnvelope("doctor", failErr == nil),
				Strict:    doctorStrict,
				Artifacts: items,
			}); err != nil {
				return err
			}
		}
		return failErr
	},
}

type doctorDoc struct {
	envelope
	Strict    bool         `json:"strict"`
	Artifacts []doctorItem `json:"artifacts"`
}

type doctorItem struct {
	Kind   string           `json:"kind"`
	Name   string           `json:"name"`
	Path   string           `json:"path"`
	Issues []doctorIssueDoc `json:"issues"`
}

type doctorIssueDoc struct {
	// Severity is "error" (always fails) or "warning" (fails under --strict).
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// doctorIssue is one preflight problem found on an artifact. fatal issues
// (a missing interpreter/binary, a missing entrypoint file) always fail the
// command; non-fatal ones (a missing "auth:" variable) only do with
// --strict, since they don't stop the artifact from being discovered or
// listed -- only from being actually invoked.
type doctorIssue struct {
	fatal   bool
	message string
}

// shellCommandFields are the frontmatter fields whose value is a shell
// command AgentWorks itself will later exec via "sh -c" (see cmd/build.go,
// cmd/test.go, cmd/eval.go, internal/targets/mcpconfig) -- doctor checks its
// interpreter/binary is resolvable now, rather than the first sign of
// trouble being a raw "command not found" from the shell later.
var shellCommandFields = []string{"command", "test", "build", "eval_runner"}

// doctorChecks runs every preflight check against a, given the environment
// (os.Environ() normally; a fake slice in tests) it would actually run in.
// Pure and side-effect-free so it's easy to unit test without a real PATH/
// filesystem dependency creeping into cmd/doctor_test.go.
func doctorChecks(a *artifact.Artifact, env []string) []doctorIssue {
	var issues []doctorIssue

	for _, field := range shellCommandFields {
		command := a.ExtraString(field)
		if command == "" {
			continue
		}
		bin := commandBin(a, field, command)
		if bin == "" {
			continue
		}
		if _, err := exec.LookPath(bin); err != nil {
			issues = append(issues, doctorIssue{
				fatal:   true,
				message: fmt.Sprintf("%q (from %q) is not on PATH", bin, field),
			})
		}
	}

	if a.Kind == artifact.KindHook {
		handlers, _ := a.HookHandlers() // a malformed declaration is validate's to report
		for _, h := range handlers {
			if h.Command == a.ExtraString("command") {
				continue // already checked above
			}
			if bin := commandBin(a, "command", h.Command); bin != "" {
				if _, err := exec.LookPath(bin); err != nil {
					issues = append(issues, doctorIssue{
						fatal:   true,
						message: fmt.Sprintf("%q (from a %s handler's command) is not on PATH", bin, h.Event),
					})
				}
			}
		}
	}

	if entrypoint := a.ExtraString("entrypoint"); entrypoint != "" {
		path := filepath.Join(a.Dir, entrypoint)
		if _, err := os.Stat(path); err != nil {
			issues = append(issues, doctorIssue{
				fatal:   true,
				message: fmt.Sprintf("entrypoint %q does not exist", path),
			})
		}
	}

	if a.Kind == artifact.KindMCP {
		if p := mcpconfig.Placeholder(a); p != "" {
			issues = append(issues, doctorIssue{
				fatal:   true,
				message: fmt.Sprintf("%q still contains a scaffold placeholder -- replace it before running or exporting", p),
			})
		}
		for _, name := range a.ExtraStringSlice("auth") {
			if !envHasValue(env, name) {
				issues = append(issues, doctorIssue{
					fatal:   false,
					message: fmt.Sprintf("environment variable %q is not set -- calling this server with 'agentworks run' will fail until it is", name),
				})
			}
		}
	}

	return issues
}

// commandBin returns the program a declared command would run, resolved the
// way it will actually be run: ${ARTIFACT_DIR} is the artifact's directory,
// quotes around the program are dropped, and a relative path resolves against
// the artifact's directory -- where test:, build:, eval_runner:, and an MCP
// server's command run. (A hook's command runs from the harness's working
// directory instead, so its relative paths are left alone.) A program with no
// slash is looked up on PATH.
func commandBin(a *artifact.Artifact, field, command string) string {
	command = strings.ReplaceAll(command, artifact.ArtifactDirVar, a.Dir)
	bin := strings.NewReplacer(`"`, "", `'`, "").Replace(firstShellWord(command))
	runsInArtifactDir := field != "command" || a.Kind != artifact.KindHook
	if runsInArtifactDir && bin != "" && !filepath.IsAbs(bin) && strings.Contains(bin, "/") {
		bin = filepath.Join(a.Dir, bin)
	}
	return bin
}

// firstShellWord returns the first whitespace-separated word of a shell
// command -- its interpreter or binary, e.g. "python3" out of "python3
// src/main.py". A plain split, not a real shell parse: good enough to
// catch the common "the interpreter isn't installed" case without
// AgentWorks growing its own shell grammar for a preflight check.
func firstShellWord(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// envHasValue reports whether name is set to a non-empty value in env
// (the "KEY=value" slice shape of os.Environ()).
func envHasValue(env []string, name string) bool {
	prefix := name + "="
	for _, e := range env {
		if rest, ok := strings.CutPrefix(e, prefix); ok {
			return rest != ""
		}
	}
	return false
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().BoolVar(&doctorStrict, "strict", false, "treat missing \"auth:\" environment variables as failures")
}
