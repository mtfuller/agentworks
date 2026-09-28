package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const helperEnvironment = "AGENTWORKS_PROCESS_TEST_HELPER"

func TestProcessHelper(t *testing.T) {
	if os.Getenv(helperEnvironment) != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		os.Exit(90)
	}
	args := os.Args[separator+1:]
	switch args[0] {
	case "success":
		fmt.Fprint(os.Stdout, "hello stdout")
		fmt.Fprint(os.Stderr, "hello stderr")
	case "exit":
		os.Exit(7)
	case "print-env":
		fmt.Fprint(os.Stdout, os.Getenv(args[1]))
	case "tree-parent":
		child := exec.Command(os.Args[0], "-test.run=TestProcessHelper", "--", "tree-child")
		child.Env = helperEnv()
		if err := child.Start(); err != nil {
			os.Exit(91)
		}
		if err := os.WriteFile(args[1], []byte(strconv.Itoa(child.Process.Pid)), 0o644); err != nil {
			os.Exit(92)
		}
		stayAlive()
	case "tree-child":
		stayAlive()
	case "ignore-graceful":
		ignoreGracefulSignal()
		if err := os.WriteFile(args[1], []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
			os.Exit(94)
		}
		stayAlive()
	default:
		os.Exit(93)
	}
	os.Exit(0)
}

func TestRunStreamsOutputAndReportsSuccess(t *testing.T) {
	var mu sync.Mutex
	var outputs []Output
	result, err := Run(context.Background(), helperSpec("success", Sink(func(output Output) {
		mu.Lock()
		defer mu.Unlock()
		outputs = append(outputs, output)
	})))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 || result.PID <= 0 || result.Duration() < 0 || result.Cancelled {
		t.Fatalf("result = %#v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(outputs) != 2 {
		t.Fatalf("outputs = %#v, want stdout and stderr", outputs)
	}
	seen := map[Stream]string{}
	for index, output := range outputs {
		if output.Sequence != uint64(index+1) {
			t.Errorf("sequence = %d at index %d", output.Sequence, index)
		}
		seen[output.Stream] += string(output.Data)
	}
	if seen[Stdout] != "hello stdout" || seen[Stderr] != "hello stderr" {
		t.Errorf("streams = %#v", seen)
	}
}

func TestRunReportsExitCode(t *testing.T) {
	result, err := Run(context.Background(), helperSpec("exit", nil))
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run() error = %v, want ExitError", err)
	}
	if result.ExitCode != 7 || exitErr.ExitCode != 7 {
		t.Fatalf("result = %#v, error = %#v", result, exitErr)
	}
}

func TestEnvironmentIsExactAndDoesNotImplicitlyInherit(t *testing.T) {
	const key = "AGENTWORKS_PROCESS_PARENT_ONLY"
	t.Setenv(key, "secret-value")
	var output strings.Builder
	spec := helperSpec("print-env", func(event Output) {
		if event.Stream == Stdout {
			output.Write(event.Data)
		}
	})
	spec.Args = append(spec.Args, key)
	spec.Env = []string{helperEnvironment + "=1"}
	if _, err := Run(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if output.String() != "" {
		t.Fatalf("child inherited parent-only environment value %q", output.String())
	}
}

func TestCancellationTerminatesCompleteProcessTree(t *testing.T) {
	if !supportsTreeTests {
		t.Skip("process-tree inspection is not supported on this platform")
	}
	childPIDFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	spec := helperSpec("tree-parent", nil)
	spec.Args = append(spec.Args, childPIDFile)
	spec.GracePeriod = 100 * time.Millisecond
	process, err := Start(ctx, spec)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	childPID := waitForPIDFile(t, childPIDFile)
	if !processExists(process.PID()) || !processExists(childPID) {
		t.Fatalf("process tree did not start: parent=%d child=%d", process.PID(), childPID)
	}

	cancel()
	result, err := process.Wait()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error = %v, want context.Canceled", err)
	}
	if !result.Cancelled {
		t.Fatalf("result = %#v, want cancelled", result)
	}
	waitForProcessGone(t, process.PID())
	waitForProcessGone(t, childPID)
}

func TestStopAndWaitAreIdempotent(t *testing.T) {
	if !supportsTreeTests {
		t.Skip("process-tree supervision is not supported on this platform")
	}
	spec := helperSpec("tree-child", nil)
	spec.GracePeriod = 20 * time.Millisecond
	process, err := Start(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	process.Stop()
	process.Stop()
	first, firstErr := process.Wait()
	second, secondErr := process.Wait()
	if first != second || fmt.Sprint(firstErr) != fmt.Sprint(secondErr) {
		t.Fatalf("Wait results differ: %#v/%v vs %#v/%v", first, firstErr, second, secondErr)
	}
	if !first.Cancelled || firstErr == nil {
		t.Fatalf("result = %#v, error = %v", first, firstErr)
	}
}

func TestStopAfterCompletionDoesNotRelabelResult(t *testing.T) {
	process, err := Start(context.Background(), helperSpec("success", nil))
	if err != nil {
		t.Fatal(err)
	}
	first, err := process.Wait()
	if err != nil {
		t.Fatal(err)
	}
	process.Stop()
	second, err := process.Wait()
	if err != nil || second.Cancelled || first != second {
		t.Fatalf("completed process changed after Stop: %#v/%v -> %#v/%v", first, nil, second, err)
	}
}

func TestCancellationForceKillsAfterGracePeriod(t *testing.T) {
	if !supportsTreeTests {
		t.Skip("process-tree supervision is not supported on this platform")
	}
	readyFile := filepath.Join(t.TempDir(), "ready.pid")
	ctx, cancel := context.WithCancel(context.Background())
	spec := helperSpec("ignore-graceful", nil)
	spec.Args = append(spec.Args, readyFile)
	spec.GracePeriod = 80 * time.Millisecond
	process, err := Start(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	waitForPIDFile(t, readyFile)
	started := time.Now()
	cancel()
	result, err := process.Wait()
	if !errors.Is(err, context.Canceled) || !result.Cancelled {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > 3*time.Second {
		t.Errorf("forced termination took %s, want approximately the 80ms grace period", elapsed)
	}
	waitForProcessGone(t, process.PID())
}

func TestStartValidatesBeforeLaunching(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		spec Spec
		want string
	}{
		{name: "executable", ctx: context.Background(), spec: Spec{}, want: "executable is required"},
		{name: "grace", ctx: context.Background(), spec: Spec{Executable: os.Args[0], GracePeriod: -1}, want: "cannot be negative"},
		{name: "context", ctx: cancelled, spec: Spec{Executable: os.Args[0]}, want: "context canceled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Start(test.ctx, test.spec)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Start() error = %v, want %q", err, test.want)
			}
		})
	}
}

func helperSpec(action string, sink Sink) Spec {
	return Spec{
		Executable: os.Args[0],
		Args:       []string{"-test.run=TestProcessHelper", "--", action},
		Env:        helperEnv(),
		Sink:       sink,
	}
}

func helperEnv() []string {
	return append(os.Environ(), helperEnvironment+"=1")
}

func stayAlive() {
	for {
		time.Sleep(time.Second)
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatalf("invalid child PID %q: %v", data, err)
			}
			return pid
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child PID file %s was not written", path)
	return 0
}

func waitForProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("process %d still exists after supervisor finished", pid)
}
