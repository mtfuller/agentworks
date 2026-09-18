package cmd

import (
	"reflect"
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestBundleDescription(t *testing.T) {
	members := []*artifact.Artifact{
		{Frontmatter: artifact.Frontmatter{Name: "a"}},
		{Frontmatter: artifact.Frontmatter{Name: "b", Namespace: "obra"}},
	}
	got := bundleDescription(members)
	want := "Bundle of 2 artifacts: a, @obra/b"
	if got != want {
		t.Errorf("bundleDescription() = %q, want %q", got, want)
	}
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV([]string{"obra, acme", "", ".", "x,,y"})
	want := []string{"obra", "acme", ".", "x", "y"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitCSV() = %v, want %v", got, want)
	}
}
