package inspector

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mtfuller/agentworks/internal/artifact"
)

// Run launches the MCP inspector for tool artifact a: starts command (the
// artifact's declared "command" frontmatter, e.g. "python3 src/main.py"
// -- see cmd/run.go) as "sh -c command" in dir with env as its process
// environment (mcpclient.StartProcess's own convention, matching
// mcpconfig.ServerFor/cmd/test.go's), and drives a full-screen Bubble Tea
// UI until the user quits. missingAuth is purely informational (a header
// warning badge for "auth:" variables cmd/run.go found unset) -- it
// doesn't block connecting, matching `agentworks doctor`'s own
// warn-don't-block treatment of the same condition.
//
// However the program exits (quit, or an error propagating out of
// tea.Program.Run), the underlying process is torn down here, after the
// Bubble Tea loop itself has stopped -- see mcpclient.Process.Close.
func Run(a *artifact.Artifact, command, dir string, env, missingAuth []string) error {
	m := New(a, command, dir, env, missingAuth)

	final, runErr := tea.NewProgram(m, tea.WithAltScreen()).Run()

	if fm, ok := final.(Model); ok && fm.proc != nil {
		if closeErr := fm.proc.Close(); closeErr != nil && runErr == nil {
			return fmt.Errorf("stopping %s: %w", a.Name, closeErr)
		}
	}
	return runErr
}
