package evalspec

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// Outcome is how one case ended.
type Outcome string

const (
	OutcomePassed  Outcome = "passed"
	OutcomeFailed  Outcome = "failed"
	OutcomeSkipped Outcome = "skipped"
)

// Result is one case's result, ready to report.
type Result struct {
	Artifact string
	Case     string
	Outcome  Outcome
	// Reasons say why a case failed or was skipped.
	Reasons  []string
	Runs     int
	Passes   int
	Duration time.Duration
}

// WriteJUnit writes results as a JUnit XML report, one <testsuite> per
// artifact, which CI systems (GitHub Actions summaries, Jenkins, GitLab, ...)
// display as test results. A multi-run case reports its pass rate in
// <system-out>.
func WriteJUnit(w io.Writer, results []Result) error {
	type failure struct {
		Message string `xml:"message,attr"`
		Type    string `xml:"type,attr"`
		Body    string `xml:",chardata"`
	}
	type skipped struct {
		Message string `xml:"message,attr,omitempty"`
	}
	type testcase struct {
		Name      string   `xml:"name,attr"`
		Classname string   `xml:"classname,attr"`
		Time      string   `xml:"time,attr"`
		Failure   *failure `xml:"failure,omitempty"`
		Skipped   *skipped `xml:"skipped,omitempty"`
		SystemOut string   `xml:"system-out,omitempty"`
	}
	type testsuite struct {
		Name     string     `xml:"name,attr"`
		Tests    int        `xml:"tests,attr"`
		Failures int        `xml:"failures,attr"`
		Skipped  int        `xml:"skipped,attr"`
		Time     string     `xml:"time,attr"`
		Cases    []testcase `xml:"testcase"`
	}
	type testsuites struct {
		XMLName  xml.Name    `xml:"testsuites"`
		Name     string      `xml:"name,attr"`
		Tests    int         `xml:"tests,attr"`
		Failures int         `xml:"failures,attr"`
		Skipped  int         `xml:"skipped,attr"`
		Suites   []testsuite `xml:"testsuite"`
	}

	doc := testsuites{Name: "agentworks eval"}
	index := map[string]int{}
	for _, r := range results {
		i, ok := index[r.Artifact]
		if !ok {
			i = len(doc.Suites)
			index[r.Artifact] = i
			doc.Suites = append(doc.Suites, testsuite{Name: r.Artifact})
		}
		suite := &doc.Suites[i]

		tc := testcase{Name: r.Case, Classname: r.Artifact, Time: seconds(r.Duration)}
		switch r.Outcome {
		case OutcomeFailed:
			suite.Failures++
			doc.Failures++
			msg := "failed"
			if len(r.Reasons) > 0 {
				msg = r.Reasons[0]
			}
			tc.Failure = &failure{Message: msg, Type: "AssertionError", Body: strings.Join(r.Reasons, "\n")}
		case OutcomeSkipped:
			suite.Skipped++
			doc.Skipped++
			tc.Skipped = &skipped{Message: strings.Join(r.Reasons, "; ")}
		}
		if r.Runs > 1 {
			tc.SystemOut = fmt.Sprintf("%d of %d runs passed", r.Passes, r.Runs)
		}
		suite.Tests++
		doc.Tests++
		suite.Cases = append(suite.Cases, tc)
	}
	for i := range doc.Suites {
		var total time.Duration
		for _, r := range results {
			if r.Artifact == doc.Suites[i].Name {
				total += r.Duration
			}
		}
		doc.Suites[i].Time = seconds(total)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func seconds(d time.Duration) string { return fmt.Sprintf("%.3f", d.Seconds()) }
