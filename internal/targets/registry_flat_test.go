package targets

import (
	"testing"

	"github.com/mtfuller/agentworks/internal/artifact"
)

func TestFlatNames(t *testing.T) {
	mk := func(kind artifact.Kind, ns, name string) *artifact.Artifact {
		return &artifact.Artifact{Frontmatter: artifact.Frontmatter{Kind: kind, Namespace: ns, Name: name}}
	}
	mine, a, b := mk(artifact.KindSkill, "", "plan"), mk(artifact.KindSkill, "obra", "plan"), mk(artifact.KindSkill, "acme", "plan")
	agent, unique := mk(artifact.KindAgent, "obra", "plan"), mk(artifact.KindSkill, "obra", "solo")

	got := FlatNames([]*artifact.Artifact{mine, a, b, agent, unique})
	want := map[*artifact.Artifact]string{
		mine:   "plan", // un-namespaced keeps its bare name; only namespaced ones get prefixed
		a:      "obra-plan",
		b:      "acme-plan",
		agent:  "plan", // a skill and an agent sharing a name don't collide: different directories
		unique: "solo",
	}
	for art, w := range want {
		if got[art] != w {
			t.Errorf("FlatNames[%s/%s] = %q, want %q", art.Namespace, art.Name, got[art], w)
		}
	}
}
