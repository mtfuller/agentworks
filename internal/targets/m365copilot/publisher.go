package m365copilot

import (
	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
)

// placeholder* are the values used for any publisher field the project
// hasn't set -- clearly labeled as TODOs (or, for the URLs, obviously
// fake) rather than something that could be mistaken for real.
const (
	placeholderDeveloperName = "TODO: replace with your publisher name"
	placeholderWebsiteURL    = "https://example.com"
	placeholderPrivacyURL    = "https://example.com/privacy"
	placeholderTermsURL      = "https://example.com/terms"
	defaultAccentColor       = "#5B5FC7"
)

// resolvedPublisher is what actually goes into the exported app manifest's
// developer block and accentColor.
type resolvedPublisher struct {
	Name        string
	Website     string
	PrivacyURL  string
	TermsURL    string
	AccentColor string
}

// publisherFor resolves an artifact's project's agentworks.yaml `publisher:`
// block (internal/project.Manifest.Publisher) into manifest-ready values,
// falling back to placeholders field-by-field for whatever isn't set.
// isPlaceholder reports whether any of the three fields Microsoft actually
// requires (developer name, privacy URL, terms URL) are still a
// placeholder, which is what determines whether cmd/export.go's
// post-export warning is still worth printing.
func publisherFor(a *artifact.Artifact) (pub resolvedPublisher, isPlaceholder bool) {
	pub = resolvedPublisher{
		Name:        placeholderDeveloperName,
		Website:     placeholderWebsiteURL,
		PrivacyURL:  placeholderPrivacyURL,
		TermsURL:    placeholderTermsURL,
		AccentColor: defaultAccentColor,
	}

	root, err := project.FindRoot(a.Dir)
	if err != nil {
		return pub, true
	}
	pm, err := project.Load(root)
	if err != nil || pm.Publisher == nil {
		return pub, true
	}

	p := pm.Publisher
	if p.Name != "" {
		pub.Name = p.Name
	}
	if p.Website != "" {
		pub.Website = p.Website
	}
	if p.PrivacyURL != "" {
		pub.PrivacyURL = p.PrivacyURL
	}
	if p.TermsURL != "" {
		pub.TermsURL = p.TermsURL
	}
	if p.AccentColor != "" {
		pub.AccentColor = p.AccentColor
	}

	isPlaceholder = p.Name == "" || p.PrivacyURL == "" || p.TermsURL == ""
	return pub, isPlaceholder
}

// UsesPlaceholderPublisher reports whether exporting a would fall back to
// placeholder developer/privacy/terms values -- i.e. whether the project's
// agentworks.yaml has (all of) a `publisher:` block set. cmd/export.go
// uses this to decide whether its post-export warning is still needed.
func UsesPlaceholderPublisher(a *artifact.Artifact) bool {
	_, isPlaceholder := publisherFor(a)
	return isPlaceholder
}
