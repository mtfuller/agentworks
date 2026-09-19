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
		{"gitlab repo is a git remote", "https://gitlab.com/owner/repo", Source{Kind: SourceGit, URL: "https://gitlab.com/owner/repo.git"}, false},
		{"bitbucket repo is a git remote", "https://bitbucket.org/owner/repo", Source{Kind: SourceGit, URL: "https://bitbucket.org/owner/repo.git"}, false},
		{"gitlab web tree url is not understood", "https://gitlab.com/owner/repo/-/tree/main/x", Source{}, true},
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

func TestParseAddArgumentGenericGitAndNPM(t *testing.T) {
	tests := []struct {
		arg  string
		want Source
	}{
		{"npm:@acme/kit", Source{Kind: SourceNPM, Package: "@acme/kit"}},
		{"npm:@acme/kit@1.2.3", Source{Kind: SourceNPM, Package: "@acme/kit", Version: "1.2.3"}},
		{"npm:kit", Source{Kind: SourceNPM, Package: "kit"}},
		{"npm:kit@2.0.0-beta.1", Source{Kind: SourceNPM, Package: "kit", Version: "2.0.0-beta.1"}},
		{"https://gitlab.com/team/plugin", Source{Kind: SourceGit, URL: "https://gitlab.com/team/plugin.git"}},
		{"https://bitbucket.org/team/plugin.git", Source{Kind: SourceGit, URL: "https://bitbucket.org/team/plugin.git"}},
		{"git@gitlab.com:team/plugin.git", Source{Kind: SourceGit, URL: "git@gitlab.com:team/plugin.git"}},
		{"git@gitlab.com:team/plugin.git#v2", Source{Kind: SourceGit, URL: "git@gitlab.com:team/plugin.git", Ref: "v2"}},
		{"https://git.example.com/x/y.git#main:plugins/one", Source{Kind: SourceGit, URL: "https://git.example.com/x/y.git", Ref: "main", Path: "plugins/one"}},
		{"git+https://git.example.com/x/y.git", Source{Kind: SourceGit, URL: "https://git.example.com/x/y.git"}},
		{"ssh://git@git.example.com/x/y.git", Source{Kind: SourceGit, URL: "ssh://git@git.example.com/x/y.git"}},
		// A GitHub URL ending in .git still takes GitHub's dependency-free path.
		{"https://github.com/owner/repo.git#v1", Source{Kind: SourceGitHub, Repo: "owner/repo", Ref: "v1"}},
	}
	for _, tt := range tests {
		got, err := ParseAddArgument(tt.arg)
		if err != nil {
			t.Errorf("ParseAddArgument(%q) error = %v", tt.arg, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseAddArgument(%q) = %+v, want %+v", tt.arg, got, tt.want)
		}
	}
}

func TestParseAddArgumentRefusesDangerousRemotes(t *testing.T) {
	for _, arg := range []string{
		"--upload-pack=touch /tmp/pwn",
		"-c",
		"ext::sh -c 'touch /tmp/pwn'",
		"file:///etc/x.git",
		"git://insecure.example.com/x/y.git",
		"npm:",
		"npm:Not_A_Valid_Name",
		"https://gitlab.com/team/plugin/-/tree/main/x",
	} {
		if src, err := ParseAddArgument(arg); err == nil {
			t.Errorf("ParseAddArgument(%q) = %+v, want an error", arg, src)
		}
	}
}

func TestDefaultNamespaceForGitAndNPM(t *testing.T) {
	tests := []struct {
		src  Source
		want string
	}{
		{Source{Kind: SourceGit, URL: "https://gitlab.com/team-a/plugin.git"}, "team-a"},
		{Source{Kind: SourceGit, URL: "git@gitlab.com:team-a/plugin.git"}, "team-a"},
		{Source{Kind: SourceNPM, Package: "@acme/kit"}, "acme"},
		{Source{Kind: SourceNPM, Package: "kit"}, "kit"},
	}
	for _, tt := range tests {
		if got := tt.src.DefaultNamespace(); got != tt.want {
			t.Errorf("DefaultNamespace(%+v) = %q, want %q", tt.src, got, tt.want)
		}
	}
}
