package inspector

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/mcpclient"
)

// Run launches the MCP inspector for mcp artifact a, connecting to target:
// a local server started as a process (cmd/run.go builds the Target from the
// artifact's command/args/env, the same execution convention `agentworks
// test` uses) or a remote http/sse endpoint. It drives a full-screen Bubble
// Tea UI until the user quits. missingAuth is purely informational (a header
// warning badge for "auth:" variables cmd/run.go found unset) -- it doesn't
// block connecting, matching `agentworks doctor`'s own warn-don't-block
// treatment of the same condition.
//
// However the program exits (quit, or an error propagating out of
// tea.Program.Run), the connection is torn down here, after the Bubble Tea
// loop itself has stopped -- for a local server that stops its process (see
// mcpclient.Process.Close), for a remote one it ends the session.
func Run(a *artifact.Artifact, target mcpclient.Target, missingAuth []string) error {
	m := New(a, target, missingAuth)

	final, runErr := tea.NewProgram(m, tea.WithAltScreen()).Run()

	if fm, ok := final.(Model); ok && fm.conn != nil {
		if closeErr := fm.conn.Close(); closeErr != nil && runErr == nil {
			return fmt.Errorf("stopping %s: %w", a.Name, closeErr)
		}
	}
	return runErr
}
