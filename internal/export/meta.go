package export

import (
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/targets"
)

// PluginMeta converts a project manifest's publisher fields to what the
// exporters write into plugin manifests. A nil manifest yields no metadata.
func PluginMeta(m *project.Manifest) targets.PluginMeta {
	if m == nil {
		return targets.PluginMeta{}
	}
	meta := targets.PluginMeta{
		Version:    m.Version,
		License:    m.License,
		Homepage:   m.Homepage,
		Repository: m.Repository,
	}
	if m.Author != nil {
		meta.Author = targets.Author{Name: m.Author.Name, Email: m.Author.Email, URL: m.Author.URL}
	}
	return meta
}
