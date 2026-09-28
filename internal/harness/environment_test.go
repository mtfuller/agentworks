package harness

import (
	"reflect"
	"testing"

	awprocess "github.com/mtfuller/agentworks/internal/process"
)

func TestRuntimeEnvironmentUsesExplicitLeastPrivilegeAllowlist(t *testing.T) {
	got := RuntimeEnvironment([]string{
		"DATABASE_URL=do-not-share",
		"AWS_SECRET_ACCESS_KEY=do-not-share",
		"PATH=/first",
		"CLAUDE_CODE_OAUTH_TOKEN=allowed",
		"LC_ALL=en_US.UTF-8",
		"PATH=/last",
		"MALFORMED",
	}, "CLAUDE_CODE_OAUTH_TOKEN")
	want := []string{
		"CLAUDE_CODE_OAUTH_TOKEN=allowed",
		"LC_ALL=en_US.UTF-8",
		"PATH=/last",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RuntimeEnvironment() = %#v, want %#v", got, want)
	}
}

func TestRedactEnvironmentSecrets(t *testing.T) {
	filter := RedactEnvironmentSecrets(nil, []string{
		"GITHUB_TOKEN=github-secret-value",
		"DATABASE_URL=database-secret-value",
		"GH_TOKEN=short",
	}, "GITHUB_TOKEN", "GH_TOKEN")
	got := string(filter(awprocess.Stderr, []byte("github-secret-value database-secret-value short")))
	want := "[REDACTED] database-secret-value short"
	if got != want {
		t.Fatalf("filtered output = %q, want %q", got, want)
	}
}

func TestRuntimeEnvironmentDoesNotImplicitlyAllowHarnessCredentials(t *testing.T) {
	got := RuntimeEnvironment([]string{"PATH=/bin", "GITHUB_TOKEN=secret"})
	want := []string{"PATH=/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RuntimeEnvironment() = %#v, want %#v", got, want)
	}
}
