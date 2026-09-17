package artifact

import (
	"fmt"
	"regexp"
	"strings"
)

// Description length bounds. The upper bound comes from the Agent Skills
// spec (https://agentskills.io/specification), which claude-code/chatgpt/
// github-copilot's skill exporters all build on ("Max 1024 characters").
// Applied to every kind, not just skills, since AgentWorks' artifact model
// is shared across kinds and it's a reasonable universal guard either way.
// The lower bound is a heuristic, not a spec rule: it catches the spec's
// own "poor example" shape ("Helps with PDFs.") without being so tight it
// nags at legitimately terse-but-clear descriptions.
const (
	descriptionMaxChars = 1024
	descriptionMinChars = 20
)

// overlapThreshold is how much two same-kind descriptions' significant
// words can share (Jaccard similarity) before LintOverlap flags them as
// likely to confuse an agent choosing between them by description alone.
const overlapThreshold = 0.7

// LintWarning is a non-fatal description-quality issue -- unlike
// Validate(), which only fails on structural problems (missing fields,
// broken references), these are heuristics about how well a description
// will actually work for an agent deciding whether to use the artifact.
type LintWarning struct {
	Dir     string
	Message string
}

var lintStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "of": true,
	"for": true, "to": true, "in": true, "on": true, "with": true,
	"that": true, "this": true, "is": true, "are": true,
}

var wordSplit = regexp.MustCompile(`[^a-z0-9]+`)

// descriptionWords returns s's significant words: lowercased, split on
// runs of non-alphanumerics, with stopwords and very short tokens (which
// are mostly noise -- "a", "of", "csv"'s neighbors) dropped.
func descriptionWords(s string) map[string]bool {
	words := map[string]bool{}
	for _, w := range wordSplit.Split(strings.ToLower(s), -1) {
		if len(w) < 3 || lintStopwords[w] {
			continue
		}
		words[w] = true
	}
	return words
}

// LintDescription checks a's description for quality issues Validate()
// doesn't catch: it's structurally fine (non-empty) but still too long,
// too vague, or redundant with the name. Never fails `validate` on its
// own -- see cmd/validate.go's --strict flag for promoting these to
// failures.
func (a *Artifact) LintDescription() []LintWarning {
	desc := strings.TrimSpace(a.Description)
	if desc == "" {
		return nil // Validate() already reports a missing description
	}

	var warnings []LintWarning
	if n := len(desc); n > descriptionMaxChars {
		warnings = append(warnings, LintWarning{a.Dir, fmt.Sprintf(
			"description is %d characters, over the Agent Skills spec's %d-character limit -- likely to be rejected or truncated on export",
			n, descriptionMaxChars)})
	} else if n < descriptionMinChars {
		warnings = append(warnings, LintWarning{a.Dir, fmt.Sprintf(
			"description %q is very short -- describe both what it does and when to use it so an agent can match it to the right task",
			desc)})
	}

	nameWords := descriptionWords(strings.ReplaceAll(a.Name, "-", " "))
	descWords := descriptionWords(desc)
	if len(nameWords) > 0 && len(descWords) > 0 && isSubset(descWords, nameWords) {
		warnings = append(warnings, LintWarning{a.Dir,
			"description adds no information beyond the name -- an agent can't tell what this does or when to use it from the description alone"})
	}

	return warnings
}

func isSubset(sub, of map[string]bool) bool {
	for w := range sub {
		if !of[w] {
			return false
		}
	}
	return true
}

// LintOverlap flags pairs of same-kind artifacts whose descriptions share
// most of their significant words -- a sign an agent choosing between them
// by description alone won't reliably tell them apart. Only meaningful
// across a whole project, so callers pass the full discovered set.
func LintOverlap(artifacts []*Artifact) []LintWarning {
	var warnings []LintWarning
	for i, a := range artifacts {
		aWords := descriptionWords(a.Description)
		if len(aWords) == 0 {
			continue
		}
		for _, b := range artifacts[i+1:] {
			if b.Kind != a.Kind {
				continue
			}
			bWords := descriptionWords(b.Description)
			if len(bWords) == 0 {
				continue
			}
			sim := jaccard(aWords, bWords)
			if sim < overlapThreshold {
				continue
			}
			warnings = append(warnings,
				LintWarning{a.Dir, fmt.Sprintf(
					"description overlaps heavily with %q (%s, %.0f%% similar words) -- an agent may struggle to pick the right one",
					b.Name, b.Kind, sim*100)},
				LintWarning{b.Dir, fmt.Sprintf(
					"description overlaps heavily with %q (%s, %.0f%% similar words) -- an agent may struggle to pick the right one",
					a.Name, a.Kind, sim*100)},
			)
		}
	}
	return warnings
}

func jaccard(a, b map[string]bool) float64 {
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
