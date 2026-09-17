package m365copilot

import (
	"bytes"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/mtfuller/agentworks/internal/artifact"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/scaffold"
	"github.com/mtfuller/agentworks/internal/targets"
)

func TestPublisherForNoProjectRoot(t *testing.T) {
	// scaffold.New with no project.Init: a bare directory, no
	// agentworks.yaml anywhere above it.
	root := t.TempDir()
	a, err := scaffold.New(root, artifact.KindSkill, "demo", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}

	pub, isPlaceholder := publisherFor(a)
	if !isPlaceholder {
		t.Error("isPlaceholder = false, want true (no project root to read a publisher from)")
	}
	if pub.Name != placeholderDeveloperName {
		t.Errorf("pub.Name = %q, want the placeholder", pub.Name)
	}
}

func TestPublisherForNoPublisherBlock(t *testing.T) {
	root := t.TempDir()
	if _, err := project.Init(root, "proj", nil); err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	a, err := scaffold.New(root, artifact.KindSkill, "demo", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}

	pub, isPlaceholder := publisherFor(a)
	if !isPlaceholder {
		t.Error("isPlaceholder = false, want true (agentworks.yaml has no publisher: block)")
	}
	if pub.AccentColor != defaultAccentColor {
		t.Errorf("pub.AccentColor = %q, want the default", pub.AccentColor)
	}
}

func TestPublisherForFullBlock(t *testing.T) {
	root := t.TempDir()
	m, err := project.Init(root, "proj", nil)
	if err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	m.Publisher = &project.Publisher{
		Name:        "Jane Doe",
		Website:     "https://example.org",
		PrivacyURL:  "https://example.org/privacy",
		TermsURL:    "https://example.org/terms",
		AccentColor: "#123456",
	}
	writeManifest(t, root, m)

	a, err := scaffold.New(root, artifact.KindSkill, "demo", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}

	pub, isPlaceholder := publisherFor(a)
	if isPlaceholder {
		t.Error("isPlaceholder = true, want false (every required field is set)")
	}
	if pub.Name != "Jane Doe" || pub.Website != "https://example.org" ||
		pub.PrivacyURL != "https://example.org/privacy" || pub.TermsURL != "https://example.org/terms" ||
		pub.AccentColor != "#123456" {
		t.Errorf("pub = %+v, want the configured values", pub)
	}
	if UsesPlaceholderPublisher(a) {
		t.Error("UsesPlaceholderPublisher() = true, want false")
	}
}

func TestPublisherForPartialBlockStillCountsAsPlaceholder(t *testing.T) {
	root := t.TempDir()
	m, err := project.Init(root, "proj", nil)
	if err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	// Only Name set -- PrivacyURL/TermsURL, which Microsoft actually
	// requires, are still missing.
	m.Publisher = &project.Publisher{Name: "Jane Doe"}
	writeManifest(t, root, m)

	a, err := scaffold.New(root, artifact.KindSkill, "demo", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}

	pub, isPlaceholder := publisherFor(a)
	if !isPlaceholder {
		t.Error("isPlaceholder = false, want true (privacy_url/terms_url still unset)")
	}
	if pub.Name != "Jane Doe" {
		t.Errorf("pub.Name = %q, want the configured value even though other fields are still placeholders", pub.Name)
	}
	if pub.PrivacyURL != placeholderPrivacyURL {
		t.Errorf("pub.PrivacyURL = %q, want the placeholder", pub.PrivacyURL)
	}
	if !UsesPlaceholderPublisher(a) {
		t.Error("UsesPlaceholderPublisher() = false, want true")
	}
}

func TestExportUsesConfiguredPublisher(t *testing.T) {
	root := t.TempDir()
	m, err := project.Init(root, "proj", nil)
	if err != nil {
		t.Fatalf("project.Init() error = %v", err)
	}
	m.Publisher = &project.Publisher{
		Name:        "Jane Doe",
		Website:     "https://example.org",
		PrivacyURL:  "https://example.org/privacy",
		TermsURL:    "https://example.org/terms",
		AccentColor: "#123456",
	}
	writeManifest(t, root, m)

	a, err := scaffold.New(root, artifact.KindSkill, "demo", scaffold.Options{Description: "x"})
	if err != nil {
		t.Fatalf("scaffold.New() error = %v", err)
	}

	dest, err := (agentExporter{}).Export(a, t.TempDir(), targets.ExportOptions{})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	var tm teamsManifest
	if err := json.Unmarshal(readZipEntry(t, dest, "manifest.json"), &tm); err != nil {
		t.Fatalf("parsing manifest.json: %v", err)
	}
	if tm.Developer.Name != "Jane Doe" {
		t.Errorf("Developer.Name = %q, want Jane Doe", tm.Developer.Name)
	}
	if tm.Developer.WebsiteURL != "https://example.org" {
		t.Errorf("Developer.WebsiteURL = %q, want https://example.org", tm.Developer.WebsiteURL)
	}
	if tm.Developer.PrivacyURL != "https://example.org/privacy" {
		t.Errorf("Developer.PrivacyURL = %q, want https://example.org/privacy", tm.Developer.PrivacyURL)
	}
	if tm.Developer.TermsOfUseURL != "https://example.org/terms" {
		t.Errorf("Developer.TermsOfUseURL = %q, want https://example.org/terms", tm.Developer.TermsOfUseURL)
	}
	if tm.AccentColor != "#123456" {
		t.Errorf("AccentColor = %q, want #123456", tm.AccentColor)
	}

	// The color icon itself should also reflect the configured accent
	// color, not the default -- decode it and check a pixel directly.
	iconData := readZipEntry(t, dest, "color.png")
	img, err := png.Decode(bytes.NewReader(iconData))
	if err != nil {
		t.Fatalf("decoding color.png: %v", err)
	}
	want, err := parseHexColor("#123456")
	if err != nil {
		t.Fatalf("parseHexColor() error = %v", err)
	}
	r, g, b, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
	if uint8(r>>8) != want.R || uint8(g>>8) != want.G || uint8(b>>8) != want.B {
		t.Errorf("color.png center pixel = rgb(%d,%d,%d), want rgb(%d,%d,%d)", r>>8, g>>8, b>>8, want.R, want.G, want.B)
	}
}

// writeManifest is a small test helper: project.Manifest has no exported
// Save method, so this overwrites the agentworks.yaml project.Init already
// wrote with m (including its Publisher block), using the same YAML shape
// project.Load reads back.
func writeManifest(t *testing.T, root string, m *project.Manifest) {
	t.Helper()
	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("marshaling manifest: %v", err)
	}
	path := filepath.Join(root, project.ManifestFile)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
