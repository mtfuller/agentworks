package cmd

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/mcpclient"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets/mcpconfig"
)

var testRemote bool

var testCmd = &cobra.Command{
	Use:   "test [path]",
	Short: "Run an artifact's declared test command",
	Long: `Shell out to the "test:" command declared in an artifact's frontmatter,
from within its directory. Works for any language -- AgentWorks doesn't run
the tests itself, it just invokes what you told it to.

With no path, runs every artifact in the project that declares a test
command; artifacts without one are skipped -- except mcp artifacts, which get
a built-in smoke test (connect, run the MCP handshake, list what the server
offers) when they declare no "test:" of their own. Remote (http/sse) servers
are smoke-tested only with --remote, since that makes network calls.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var toRun []*artifact.Artifact

		if len(args) == 1 {
			a, err := loadArtifactAtPath(args[0])
			if err != nil {
				return err
			}
			toRun = append(toRun, a)
		} else {
			root, err := projectRoot()
			if err != nil {
				return err
			}
			found, errs := project.Discover(root)
			for _, e := range errs {
				color.Warning("%v", e)
			}
			toRun = found
		}

		ran, failed := 0, 0
		results := []testItem{}
		for _, a := range toRun {
			item := testItem{Kind: string(a.Kind), Name: a.Name, Path: itemPath(a)}
			testCommand := a.ExtraString("test")
			if testCommand == "" {
				if smokeEligible(a) {
					ran++
					item.Type = "smoke"
					if skipped, err := smokeTestMCP(a); err != nil {
						color.Error("%s: mcp smoke test failed: %v", a.Name, err)
						failed++
						item.Status, item.Message = "failed", err.Error()
					} else if skipped != "" {
						ran--
						item.Status, item.Message = "skipped", skipped
					} else {
						item.Status = "passed"
					}
					results = append(results, item)
				}
				continue
			}
			ran++
			item.Type = "test"
			color.Info("Running tests for %s (%s): %s", a.Name, a.Kind, testCommand)

			c := exec.Command("sh", "-c", testCommand)
			c.Dir = a.Dir
			c.Stdout = stdoutForChildren()
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				color.Error("%s: tests failed: %v", a.Name, err)
				failed++
				item.Status, item.Message = "failed", err.Error()
				results = append(results, item)
				continue
			}
			color.Success("%s: tests passed", a.Name)
			item.Status = "passed"
			results = append(results, item)
		}

		if ran == 0 {
			color.Info("No artifacts declare a `test:` command.")
		}
		var failErr error
		if failed > 0 {
			failErr = fmt.Errorf("%d artifact(s) failed tests", failed)
		}
		if jsonFlag {
			if err := emitJSON(testDoc{
				envelope: newEnvelope("test", failErr == nil),
				Summary:  testSummary{Ran: ran, Failed: failed},
				Results:  results,
			}); err != nil {
				return err
			}
		}
		return failErr
	},
}

// smokeEligible reports whether a is an mcp server that should get the
// built-in smoke test in place of a declared `test:` command: a local server
// with a command, or -- only when --remote was passed, since it makes network
// calls -- a remote one.
func smokeEligible(a *artifact.Artifact) bool {
	if a.Kind != artifact.KindMCP || mcpconfig.Placeholder(a) != "" {
		return false
	}
	if mcpconfig.IsRemote(a) {
		return testRemote && a.ExtraString("url") != ""
	}
	return mcpconfig.CommandLine(a) != ""
}

// smokeTestMCP connects to the mcp artifact's server, runs the MCP initialize
// handshake, and lists what it offers (tools, and resources and prompts if it
// advertises them). It's what `agentworks test` does for an mcp artifact with
// no `test:` of its own -- proof the server starts and speaks the protocol, not
// a test of its tools' behavior. Skipped (with a warning, not a failure) when a
// declared `auth:` variable isn't set, since the server can't be expected to
// work without it.
//
// Returns a non-empty skipped reason when the test was skipped, an error when
// it failed, and ("", nil) when it passed.
func smokeTestMCP(a *artifact.Artifact) (skipped string, err error) {
	env := os.Environ()
	for _, name := range a.ExtraStringSlice("auth") {
		if !envHasValue(env, name) {
			reason := fmt.Sprintf("%q is not set", name)
			color.Warning("%s: skipping mcp smoke test -- %s", a.Name, reason)
			return reason, nil
		}
	}

	target, missing := mcpTarget(a)
	if len(missing) > 0 {
		reason := fmt.Sprintf("%q is not set", missing[0])
		color.Warning("%s: skipping mcp smoke test -- %s", a.Name, reason)
		return reason, nil
	}

	if target.IsRemote() {
		color.Info("Smoke-testing remote mcp server %s (%s): %s", a.Name, target.Transport, hostOf(target.URL))
	} else {
		color.Info("Smoke-testing mcp server %s: %s", a.Name, target.Command)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var stderr []string
	res, probeErr := mcpclient.Probe(ctx, target, func(line string) { stderr = append(stderr, line) })
	if probeErr != nil {
		for _, line := range stderr {
			color.Warning("  stderr: %s", line)
		}
		return "", probeErr
	}
	color.Success("%s: %s %s started and lists %s", a.Name, res.Info.ServerInfo.Name, res.Info.ServerInfo.Version, offered(res))
	return "", nil
}

// offered summarizes what a probed server exposes, e.g. "3 tool(s), 1 resource(s)".
func offered(res mcpclient.ProbeResult) string {
	parts := []string{fmt.Sprintf("%d tool(s)", len(res.Tools))}
	if len(res.Resources) > 0 {
		parts = append(parts, fmt.Sprintf("%d resource(s)", len(res.Resources)))
	}
	if len(res.Prompts) > 0 {
		parts = append(parts, fmt.Sprintf("%d prompt(s)", len(res.Prompts)))
	}
	return strings.Join(parts, ", ")
}

// hostOf returns just the host of a URL, for display: the path, query, and
// userinfo can carry a credential.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "(remote server)"
	}
	return u.Scheme + "://" + u.Host
}

type testDoc struct {
	envelope
	Summary testSummary `json:"summary"`
	Results []testItem  `json:"results"`
}

type testSummary struct {
	Ran    int `json:"ran"`
	Failed int `json:"failed"`
}

type testItem struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Path string `json:"path"`
	// Type is "test" (the artifact's own test: command) or "smoke" (the
	// built-in mcp handshake check).
	Type string `json:"type"`
	// Status is "passed", "failed", or "skipped".
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func init() {
	rootCmd.AddCommand(testCmd)
	testCmd.Flags().BoolVar(&testRemote, "remote", false, "also smoke-test remote (http/sse) mcp servers, which makes network calls")
}
