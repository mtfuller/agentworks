package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Semantic color palette for the whole package. Kept separate from
// internal/color (raw ANSI escape strings used by the plain CLI output) --
// lipgloss needs its own Color/AdaptiveColor values to compose with the
// styles below, so the two palettes intentionally don't share code, just
// the same meaning (green = success, red = error, etc).
//
// accentColor sits near bubbles' own default list-selection color (the
// 205/212 magenta-pink family), so the tab bar and list selection highlight
// read as the same "this is active/selected" language instead of clashing.
var (
	accentColor = lipgloss.AdaptiveColor{Light: "#8839B0", Dark: "#C48CE0"}
	mutedColor  = lipgloss.AdaptiveColor{Light: "#7D7D7D", Dark: "#8A8A8A"}

	successColor = lipgloss.AdaptiveColor{Light: "#1A7A32", Dark: "#4DD26A"}
	errorColor   = lipgloss.AdaptiveColor{Light: "#B3261E", Dark: "#F2555A"}
	warnColor    = lipgloss.AdaptiveColor{Light: "#A85D00", Dark: "#E0A32E"}
	infoColor    = lipgloss.AdaptiveColor{Light: "#1B6FB3", Dark: "#5BA8E0"}
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	helpStyle    = lipgloss.NewStyle().Foreground(mutedColor)
	helpKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(accentColor)

	statusSuccessStyle = lipgloss.NewStyle().Bold(true).Foreground(successColor)
	statusErrorStyle   = lipgloss.NewStyle().Bold(true).Foreground(errorColor)
	statusWarnStyle    = lipgloss.NewStyle().Bold(true).Foreground(warnColor)
	statusInfoStyle    = lipgloss.NewStyle().Bold(true).Foreground(infoColor)

	metaLabelStyle = lipgloss.NewStyle().Foreground(mutedColor)

	activeTabStyle   = lipgloss.NewStyle().Bold(true).Foreground(accentColor).Padding(0, 2)
	inactiveTabStyle = lipgloss.NewStyle().Foreground(mutedColor).Padding(0, 2)
	tabRuleStyle     = lipgloss.NewStyle().Foreground(accentColor)
)

// statusLevel is the severity of Model.statusMsg, so the footer status line
// can be colored by outcome instead of every message (success, failure,
// warning, in-progress) rendering identically.
type statusLevel int

const (
	statusNone statusLevel = iota
	statusInfo
	statusSuccess
	statusWarn
	statusError
)

// styleFor returns the style the footer status line should render msg
// with, given its level.
func (l statusLevel) style() lipgloss.Style {
	switch l {
	case statusSuccess:
		return statusSuccessStyle
	case statusError:
		return statusErrorStyle
	case statusWarn:
		return statusWarnStyle
	default:
		return statusInfoStyle
	}
}

// setStatus sets both statusMsg and its severity level together, so the two
// can never drift out of sync the way two separate assignments could.
func (m *Model) setStatus(level statusLevel, format string, args ...any) {
	m.statusMsg = fmt.Sprintf(format, args...)
	m.statusLevel = level
}

// clearStatus resets the footer status line, e.g. when opening a pane that
// shouldn't carry over the previous action's outcome.
func (m *Model) clearStatus() {
	m.statusMsg = ""
	m.statusLevel = statusNone
}

// helpEntry is one "key: description" pair in the footer hint line.
type helpEntry struct{ key, desc string }

// renderHelp styles a pane's help entries -- the key bold/accented, the
// description muted -- and joins them with a muted separator. Each piece
// is rendered independently (rather than composing one big string and
// wrapping it in a single Style.Render call) because lipgloss styles reset
// every attribute at their end: nesting a differently-styled key inside an
// outer-styled sentence would clear the outer color partway through.
func renderHelp(entries []helpEntry) string {
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = helpKeyStyle.Render(e.key) + helpStyle.Render(": "+e.desc)
	}
	return strings.Join(parts, helpStyle.Render("  •  "))
}
