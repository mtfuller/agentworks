package marketplace

import (
	"context"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/importer"
)

// PreviewItem is one artifact a plugin would import.
type PreviewItem struct {
	Kind        artifact.Kind
	Namespace   string
	Name        string
	Description string
}

// Preview is what importing a plugin would add, without adding it.
type Preview struct {
	// Namespace is what everything in Items would be filed under.
	Namespace   string
	Items       []PreviewItem
	Unsupported []string
	// Warnings are things that would import but need attention (an ignored
	// field, a file a command references that isn't in the plugin).
	Warnings []string
}

// FetchPreview downloads src and reports what importing it would create.
// Nothing is written into root: the plan is only inspected, then its temp
// download is removed. root is used solely to compute would-be paths.
func FetchPreview(ctx context.Context, root string, src importer.Source) (*Preview, error) {
	plan, err := importer.Prepare(ctx, root, src, importer.Options{})
	if err != nil {
		return nil, err
	}
	defer plan.Close()
	return previewFromPlan(plan, src), nil
}

func previewFromPlan(plan *importer.Plan, src importer.Source) *Preview {
	p := &Preview{Namespace: src.DefaultNamespace(), Unsupported: plan.Unsupported, Warnings: plan.Warnings}
	for _, a := range plan.Artifacts {
		p.Items = append(p.Items, PreviewItem{Kind: a.Kind, Namespace: a.Namespace, Name: a.Name, Description: a.Description})
	}
	return p
}
