package artifact

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Ref is a reference from one artifact to another: `requires: [skill:csv-analyzer,
// mcp:team-a/jira-fetch]`. Namespace is "" for an unnamespaced artifact.
type Ref struct {
	Kind      Kind
	Namespace string
	Name      string
}

// String is the reference as written in frontmatter.
func (r Ref) String() string {
	if r.Namespace == "" {
		return string(r.Kind) + ":" + r.Name
	}
	return string(r.Kind) + ":" + r.Namespace + "/" + r.Name
}

// ParseRef parses "kind:name" or "kind:namespace/name" (a leading "@" on the
// namespace, the display form, is accepted).
func ParseRef(s string) (Ref, error) {
	kind, rest, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || rest == "" {
		return Ref{}, fmt.Errorf("%q is not a reference: write it as kind:name, e.g. skill:csv-analyzer", s)
	}
	k, err := ParseKind(kind)
	if err != nil {
		return Ref{}, fmt.Errorf("%q: %w", s, err)
	}
	r := Ref{Kind: k, Name: rest}
	if ns, name, has := strings.Cut(strings.TrimPrefix(rest, "@"), "/"); has {
		r.Namespace, r.Name = ns, name
	}
	if !namePattern.MatchString(r.Name) || (r.Namespace != "" && !namePattern.MatchString(r.Namespace)) {
		return Ref{}, fmt.Errorf("%q: names are lowercase letters, digits, and hyphens", s)
	}
	return r, nil
}

// Requires returns the artifact's parsed `requires:` references. Entries that
// don't parse are skipped here and reported by CheckRequires and validate.
func (a *Artifact) Requires() []Ref {
	var refs []Ref
	for _, s := range a.ExtraStringSlice("requires") {
		if r, err := ParseRef(s); err == nil {
			refs = append(refs, r)
		}
	}
	return refs
}

// Ref returns a's own reference.
func (a *Artifact) Ref() Ref { return Ref{Kind: a.Kind, Namespace: a.Namespace, Name: a.Name} }

// RequiresSyntaxErrors reports `requires:` entries that aren't valid references.
func (a *Artifact) RequiresSyntaxErrors() []error {
	var errs []error
	for _, s := range a.ExtraStringSlice("requires") {
		if r, err := ParseRef(s); err != nil {
			errs = append(errs, err)
		} else if r == a.Ref() {
			errs = append(errs, fmt.Errorf("%q: an artifact can't require itself", s))
		}
	}
	return errs
}

// CheckRequires validates every `requires:` reference across arts: each must
// name an artifact that exists, and the graph must have no cycle. It returns
// one message per problem, prefixed with the artifact's directory.
func CheckRequires(arts []*Artifact) []string {
	byRef := map[Ref]*Artifact{}
	for _, a := range arts {
		byRef[a.Ref()] = a
	}

	var problems []string
	for _, a := range arts {
		for _, r := range a.Requires() {
			if r == a.Ref() {
				continue // reported as a syntax error
			}
			if _, ok := byRef[r]; !ok {
				problems = append(problems, fmt.Sprintf("%s: requires %s, which doesn't exist in this project%s", a.Dir, r, didYouMean(r, byRef)))
			}
		}
	}

	// Cycle detection: depth-first, reporting each cycle once at its first
	// member in path order.
	const (
		visiting = 1
		done     = 2
	)
	state := map[Ref]int{}
	var path []Ref
	var visit func(a *Artifact)
	visit = func(a *Artifact) {
		state[a.Ref()] = visiting
		path = append(path, a.Ref())
		for _, r := range a.Requires() {
			dep, ok := byRef[r]
			if !ok || r == a.Ref() {
				continue
			}
			switch state[r] {
			case visiting:
				start := 0
				for i, p := range path {
					if p == r {
						start = i
					}
				}
				cycle := make([]string, 0, len(path)-start+1)
				for _, p := range path[start:] {
					cycle = append(cycle, p.String())
				}
				cycle = append(cycle, r.String())
				problems = append(problems, fmt.Sprintf("%s: dependency cycle: %s", a.Dir, strings.Join(cycle, " -> ")))
			case 0:
				visit(dep)
			}
		}
		path = path[:len(path)-1]
		state[a.Ref()] = done
	}
	sorted := append([]*Artifact(nil), arts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Dir < sorted[j].Dir })
	for _, a := range sorted {
		if state[a.Ref()] == 0 {
			visit(a)
		}
	}
	return problems
}

// didYouMean points at an artifact with the same name under another kind or
// namespace, the usual cause of a dangling reference.
func didYouMean(want Ref, have map[Ref]*Artifact) string {
	var hints []string
	for r := range have {
		if r.Name == want.Name {
			hints = append(hints, r.String())
		}
	}
	if len(hints) == 0 {
		return ""
	}
	sort.Strings(hints)
	return " (did you mean " + strings.Join(hints, " or ") + "?)"
}

// Missing returns the references of a that aren't among members: what a
// bundle holding just members would leave dangling.
func (a *Artifact) Missing(members []*Artifact) []Ref {
	have := map[Ref]bool{}
	for _, m := range members {
		have[m.Ref()] = true
	}
	var out []Ref
	for _, r := range a.Requires() {
		if !have[r] {
			out = append(out, r)
		}
	}
	return out
}

// BodyWithRequirements is the artifact's body plus, for an agent that
// declares `requires:`, a section naming what it depends on. Neither Claude
// Code's nor Copilot's subagent format has a field for depending on other
// artifacts, so the dependency is stated in the agent's own instructions.
func (a *Artifact) BodyWithRequirements() string {
	refs := a.Requires()
	if a.Kind != KindAgent || len(refs) == 0 {
		return a.Body
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(a.Body, "\n"))
	b.WriteString("\n\n## Requires\n\nThis agent relies on these being installed alongside it:\n\n")
	for _, r := range refs {
		fmt.Fprintf(&b, "- %s `%s`\n", r.Kind, strings.TrimPrefix(r.String(), string(r.Kind)+":"))
	}
	return b.String()
}

// BinRequirement is one `bins:` entry: an executable that must be on PATH,
// optionally with a minimum version ("node>=20", "python3>=3.10").
type BinRequirement struct {
	Name       string
	MinVersion string // "" when unconstrained
}

var binPattern = regexp.MustCompile(`^([A-Za-z0-9._+-]+)(?:>=([0-9]+(?:\.[0-9]+)*))?$`)

// ParseBin parses "name" or "name>=version".
func ParseBin(s string) (BinRequirement, error) {
	m := binPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return BinRequirement{}, fmt.Errorf("%q is not a binary requirement: write name or name>=version, e.g. node>=20", s)
	}
	return BinRequirement{Name: m[1], MinVersion: m[2]}, nil
}

// Bins returns the artifact's parsed `bins:` entries (unparseable ones are
// skipped; validate reports them).
func (a *Artifact) Bins() []BinRequirement {
	var out []BinRequirement
	for _, s := range a.ExtraStringSlice("bins") {
		if b, err := ParseBin(s); err == nil {
			out = append(out, b)
		}
	}
	return out
}

// BinsSyntaxErrors reports `bins:` entries that don't parse.
func (a *Artifact) BinsSyntaxErrors() []error {
	var errs []error
	for _, s := range a.ExtraStringSlice("bins") {
		if _, err := ParseBin(s); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

var versionPattern = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)+|[0-9]+`)

// CheckBin resolves b on PATH and, when a minimum is set, runs
// `<bin> --version` and compares. It returns "" when satisfied, else why not.
func CheckBin(b BinRequirement) string {
	path, err := exec.LookPath(b.Name)
	if err != nil {
		return fmt.Sprintf("%q is required (bins) but is not on PATH", b.Name)
	}
	if b.MinVersion == "" {
		return ""
	}
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return fmt.Sprintf("%q is required at >=%s but `%s --version` failed", b.Name, b.MinVersion, b.Name)
	}
	got := versionPattern.FindString(string(out))
	if got == "" {
		return fmt.Sprintf("%q is required at >=%s but its version couldn't be read from %q", b.Name, b.MinVersion, strings.TrimSpace(string(out)))
	}
	if CompareVersions(got, b.MinVersion) < 0 {
		return fmt.Sprintf("%q is version %s; >=%s is required (bins)", b.Name, got, b.MinVersion)
	}
	return ""
}

// CompareVersions compares dotted numeric versions ("3.10.2" vs "3.9"),
// treating a missing component as 0. It returns -1, 0, or 1.
func CompareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
