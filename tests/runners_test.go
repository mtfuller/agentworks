package tests

import (
	"os/exec"
	"testing"
)

// The reference eval runners in examples/eval-runners are plain Python; their
// parsing is tested with fixtures (no network, no claude), and this runs those
// tests as part of the suite.
func TestReferenceEvalRunnersPass(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	out, err := exec.Command("python3", "-m", "unittest", "discover", "-s", "../examples/eval-runners").CombinedOutput()
	if err != nil {
		t.Fatalf("the reference runners' tests failed: %v\n%s", err, out)
	}
}
