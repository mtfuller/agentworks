package runtimepath

import (
	"path/filepath"
	"testing"
)

func TestDataRootByPlatform(t *testing.T) {
	env := map[string]string{
		"LOCALAPPDATA":   filepath.Join("volume", "Local"),
		"XDG_STATE_HOME": filepath.Join("volume", "state"),
	}
	getenv := func(name string) string { return env[name] }
	tests := []struct {
		goos string
		want string
	}{
		{"darwin", filepath.Join("home", "dev", "Library", "Application Support", "AgentWorks")},
		{"windows", filepath.Join("volume", "Local", "AgentWorks")},
		{"linux", filepath.Join("volume", "state", "agentworks")},
	}
	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			got, err := dataRoot(test.goos, filepath.Join("home", "dev"), getenv)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("dataRoot = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDataRootFallbacks(t *testing.T) {
	emptyEnv := func(string) string { return "" }
	windows, err := dataRoot("windows", filepath.Join("home", "dev"), emptyEnv)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("home", "dev", "AppData", "Local", "AgentWorks"); windows != want {
		t.Fatalf("windows fallback = %q, want %q", windows, want)
	}
	linux, err := dataRoot("linux", filepath.Join("home", "dev"), emptyEnv)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("home", "dev", ".local", "state", "agentworks"); linux != want {
		t.Fatalf("linux fallback = %q, want %q", linux, want)
	}
	if _, err := dataRoot("linux", "", emptyEnv); err == nil {
		t.Fatal("empty home accepted")
	}
}

func TestProjectDirectoryIsStableAndSeparated(t *testing.T) {
	first, err := ProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProjectDir(filepath.Dir(filepath.Dir(first)))
	if err != nil {
		t.Fatal(err)
	}
	againRoot := t.TempDir()
	again, err := ProjectDir(againRoot)
	if err != nil {
		t.Fatal(err)
	}
	if first == again {
		t.Fatalf("different projects share directory %q", first)
	}
	if first == second {
		t.Fatalf("nested path unexpectedly shares directory %q", first)
	}
	oneMore, err := ProjectDir(againRoot)
	if err != nil {
		t.Fatal(err)
	}
	if oneMore != again {
		t.Fatalf("project directory is unstable: %q != %q", oneMore, again)
	}
}

func TestDatabaseAndLogPathsShareProjectDirectory(t *testing.T) {
	root := t.TempDir()
	database, err := DatabasePath(root)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := LogRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(database) != filepath.Dir(logs) {
		t.Fatalf("database=%q logs=%q", database, logs)
	}
	if filepath.Base(database) != "state.db" || filepath.Base(logs) != "logs" {
		t.Fatalf("database=%q logs=%q", database, logs)
	}
	if _, err := ProjectDir(""); err == nil {
		t.Fatal("empty project root accepted")
	}
}
