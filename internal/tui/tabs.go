package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// tabBarHeight is how many lines tabBar.View occupies (the tab row plus
// its separating rule), so callers can subtract it from the space left for
// the pane's own content.
const tabBarHeight = 2

// tabBar is a small hand-rolled horizontal tab strip -- bubbles has no
// built-in tabs widget. Used both by the main browse pane (one tab per
// artifact.Kind) and the templates pane (same kinds, independent
// selection), so active/labels stay generic rather than kind-specific.
type tabBar struct {
	labels []string
	active int
}

func newTabBar(labels []string) tabBar {
	return tabBar{labels: labels}
}

func (t *tabBar) next() {
	if len(t.labels) == 0 {
		return
	}
	t.active = (t.active + 1) % len(t.labels)
}

func (t *tabBar) prev() {
	if len(t.labels) == 0 {
		return
	}
	t.active = (t.active - 1 + len(t.labels)) % len(t.labels)
}

// View renders the tab strip: each label padded and styled (active vs.
// inactive), followed by a full-width rule separating it from whatever
// pane content renders beneath it.
func (t tabBar) View(width int) string {
	rendered := make([]string, len(t.labels))
	for i, label := range t.labels {
		if i == t.active {
			rendered[i] = activeTabStyle.Render(label)
		} else {
			rendered[i] = inactiveTabStyle.Render(label)
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, rendered...)

	rule := ""
	if width > 0 {
		rule = tabRuleStyle.Render(strings.Repeat("─", width))
	}
	return row + "\n" + rule
}
