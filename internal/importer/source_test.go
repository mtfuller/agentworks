package importer

import "testing"

func TestParseAddArgument(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		want    Source
		wantErr bool
	}{
		{"owner/repo shorthand", "owner/repo", Source{Kind: SourceGitHub, Repo: "owner/repo"}, false},
		{"github repo URL", "https://github.com/owner/repo", Source{Kind: SourceGitHub, Repo: "owner/repo"}, false},
		{"github repo URL with .git", "https://github.com/owner/repo.git", Source{Kind: SourceGitHub, Repo: "owner/repo"}, false},
		{
			"github tree URL with ref and path", "https://github.com/owner/repo/tree/main/skills/pdf",
			Source{Kind: SourceGitHub, Repo: "owner/repo", Ref: "main", Path: "skills/pdf"}, false,
		},
		{
			"github blob URL takes the dir of the file", "https://github.com/owner/repo/blob/main/skills/pdf/SKILL.md",
			Source{Kind: SourceGitHub, Repo: "owner/repo", Ref: "main", Path: "skills/pdf"}, false,
		},
		{
			"raw.githubusercontent URL takes the dir of the file",
			"https://raw.githubusercontent.com/owner/repo/main/skills/pdf/SKILL.md",
			Source{Kind: SourceGitHub, Repo: "owner/repo", Ref: "main", Path: "skills/pdf"}, false,
		},
		{"zip URL", "https://example.com/skill.zip", Source{Kind: SourceArchive, URL: "https://example.com/skill.zip"}, false},
		{"tar.gz URL", "https://example.com/skill.tar.gz", Source{Kind: SourceArchive, URL: "https://example.com/skill.tar.gz"}, false},
		{
			"agentskills.codes download URL (no extension)", "https://agentskills.codes/api/skills/download/42",
			Source{Kind: SourceArchive, URL: "https://agentskills.codes/api/skills/download/42"}, false,
		},
		{"gitlab is unsupported", "https://gitlab.com/owner/repo", Source{}, true},
		{"bitbucket is unsupported", "https://bitbucket.org/owner/repo", Source{}, true},
		{"unrecognized host with no archive extension", "https://example.com/some/page", Source{}, true},
		{"garbage", "not a url or shorthand!!", Source{}, true},
		{"empty", "", Source{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAddArgument(tt.arg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseAddArgument(%q) error = %v, wantErr %v", tt.arg, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("ParseAddArgument(%q) = %+v, want %+v", tt.arg, got, tt.want)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"Azure DevOps", "azure-devops", false},
		{"PDF_Tools", "pdf-tools", false},
		{"a--b", "a-b", false},
		{"--leading-and-trailing--", "leading-and-trailing", false},
		{"csv-analyzer", "csv-analyzer", false},
		{"!!!", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := slugify(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("slugify(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
