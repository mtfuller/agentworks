package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/mtfuller/agentworks/internal/color"
)

// The commands are Cobra commands with package-level flag variables, so an
// in-process test has to (a) reset every flag to its default between runs and
// (b) capture what a command prints, which goes to os.Stdout directly, through
// internal/color, or -- under --json -- to a single document on stdout with
// messages on stderr.

var harnessMu sync.Mutex

// result is what one in-process command run produced.
type result struct {
	stdout string
	stderr string
	err    error
}

// combined is stdout and stderr together, for assertions that don't care which
// stream a message used.
func (r result) combined() string { return r.stdout + r.stderr }

// runCLI runs `agentworks <args> --project dir` in this process.
func runCLI(t *testing.T, dir string, args ...string) result {
	t.Helper()
	return runCLIRaw(t, append(args, "--project", dir)...)
}

func runCLIRaw(t *testing.T, args ...string) result {
	t.Helper()
	harnessMu.Lock()
	defer harnessMu.Unlock()

	resetFlags(rootCmd)
	jsonEmitted = false
	checkedRoots = map[string]bool{}

	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = outW, errW
	prevColor := color.Output()
	color.SetOutput(outW)

	var outBuf, errBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(&outBuf, outR) }()
	go func() { defer wg.Done(); io.Copy(&errBuf, errR) }()

	rootCmd.SetArgs(args)
	rootCmd.SetOut(outW)
	rootCmd.SetErr(errW)
	_, err := rootCmd.ExecuteC()
	// Mirror Execute(): a failure is reported (and, under --json, becomes an
	// error document) after the command returns.
	if err != nil {
		reportError("agentworks", err)
	}

	outW.Close()
	errW.Close()
	wg.Wait()
	os.Stdout, os.Stderr = oldOut, oldErr
	color.SetOutput(prevColor)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)

	return result{stdout: outBuf.String(), stderr: errBuf.String(), err: err}
}

func resetFlags(c *cobra.Command) {
	reset := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) {
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				sv.Replace(nil)
			} else {
				f.Value.Set(f.DefValue)
			}
			f.Changed = false
		})
	}
	reset(c.Flags())
	reset(c.PersistentFlags())
	for _, sub := range c.Commands() {
		resetFlags(sub)
	}
}

// mustRun runs a command that is expected to succeed.
func mustRun(t *testing.T, dir string, args ...string) result {
	t.Helper()
	r := runCLI(t, dir, args...)
	if r.err != nil {
		t.Fatalf("agentworks %s failed: %v\n%s", strings.Join(args, " "), r.err, r.combined())
	}
	return r
}

// newProject makes an initialized project in a temp directory.
func newProject(t *testing.T, targets ...string) string {
	t.Helper()
	dir := t.TempDir()
	args := []string{"init", dir}
	for _, tg := range targets {
		args = append(args, "--target", tg)
	}
	if r := runCLIRaw(t, args...); r.err != nil {
		t.Fatalf("init failed: %v\n%s", r.err, r.combined())
	}
	return dir
}

// pluginTarGz builds a .tar.gz of files under one top-level directory, the way
// a GitHub tarball is laid out; a ".sh" path is written executable.
func pluginTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		mode := int64(0o644)
		if strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: "kit-main/" + name, Mode: mode, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(content))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}
