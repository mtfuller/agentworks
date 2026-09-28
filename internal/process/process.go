// Package process supervises child processes for the AgentWorks runtime.
// It uses structured argv, streams normalized output, and owns the complete
// process tree so cancellation cannot leave harness or tool descendants behind.
package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

const DefaultGracePeriod = 8 * time.Second

var ErrStopped = errors.New("process stopped")

type Stream string

const (
	Stdout Stream = "stdout"
	Stderr Stream = "stderr"
)

// Output is one ordered chunk emitted by a supervised process. Sink callbacks
// are serialized; they should return quickly so the child is not backpressured.
type Output struct {
	Sequence uint64
	Stream   Stream
	Data     []byte
	Time     time.Time
}

type Sink func(Output)

// Filter transforms one serialized output chunk before it reaches the sink.
// Returning an empty slice suppresses the chunk. A filter may buffer partial
// protocol records because emitter calls it serially.
type Filter func(Stream, []byte) []byte

// Spec describes one process without invoking a shell implicitly. Callers that
// intentionally need a shell must name it as Executable and pass its arguments.
// Env is exact: nil means an empty environment, not inheritance.
type Spec struct {
	Executable  string
	Args        []string
	Dir         string
	Env         []string
	Stdin       io.Reader
	GracePeriod time.Duration
	Filter      Filter
	Sink        Sink
}

type Result struct {
	PID        int
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
	Cancelled  bool
}

func (result Result) Duration() time.Duration {
	return result.FinishedAt.Sub(result.StartedAt)
}

// ExitError reports a child that started successfully but exited non-zero.
type ExitError struct {
	Executable string
	ExitCode   int
	Cause      error
}

func (err *ExitError) Error() string {
	if err.Cause != nil {
		return fmt.Sprintf("%s exited with code %d: %v", err.Executable, err.ExitCode, err.Cause)
	}
	return fmt.Sprintf("%s exited with code %d", err.Executable, err.ExitCode)
}

func (err *ExitError) Unwrap() error { return err.Cause }

type Process struct {
	cmd   *exec.Cmd
	tree  treeController
	spec  Spec
	done  chan struct{}
	start time.Time

	stopOnce sync.Once
	mu       sync.Mutex
	result   Result
	err      error
	cancel   error
	finished bool
}

// Start validates and launches a process, then watches ctx for cancellation.
func Start(ctx context.Context, spec Spec) (*Process, error) {
	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if spec.GracePeriod == 0 {
		spec.GracePeriod = DefaultGracePeriod
	}

	cmd := exec.Command(spec.Executable, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = make([]string, len(spec.Env))
	copy(cmd.Env, spec.Env)
	cmd.Stdin = spec.Stdin
	cmd.WaitDelay = spec.GracePeriod
	prepareCommand(cmd)

	emitter := &emitter{sink: spec.Sink, filter: spec.Filter}
	cmd.Stdout = streamWriter{stream: Stdout, emitter: emitter}
	cmd.Stderr = streamWriter{stream: Stderr, emitter: emitter}

	startedAt := time.Now().UTC()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %q: %w", spec.Executable, err)
	}
	tree, err := attachTree(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("supervise process tree for %q: %w", spec.Executable, err)
	}

	process := &Process{cmd: cmd, tree: tree, spec: spec, done: make(chan struct{}), start: startedAt}
	go process.wait()
	go process.watch(ctx)
	return process, nil
}

// Run starts and waits for one process.
func Run(ctx context.Context, spec Spec) (Result, error) {
	process, err := Start(ctx, spec)
	if err != nil {
		return Result{}, err
	}
	return process.Wait()
}

// Stop requests graceful termination and force-kills the complete tree after
// the configured grace period. It is safe to call more than once.
func (process *Process) Stop() {
	process.stop(ErrStopped)
}

// Wait may be called more than once and always returns the same result.
func (process *Process) Wait() (Result, error) {
	<-process.done
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.result, process.err
}

func (process *Process) PID() int { return process.cmd.Process.Pid }

func (process *Process) watch(ctx context.Context) {
	select {
	case <-process.done:
		return
	case <-ctx.Done():
		// Prefer a completed process if completion and cancellation became ready
		// together; a successful run must not be relabelled cancelled afterward.
		select {
		case <-process.done:
			return
		default:
		}
		process.stop(ctx.Err())
	}
}

func (process *Process) stop(cause error) {
	process.stopOnce.Do(func() {
		process.mu.Lock()
		if process.finished {
			process.mu.Unlock()
			return
		}
		process.cancel = cause
		process.mu.Unlock()

		_ = process.tree.Graceful()
		timer := time.NewTimer(process.spec.GracePeriod)
		defer timer.Stop()
		select {
		case <-process.done:
			return
		case <-timer.C:
			_ = process.tree.Kill()
		}
	})
}

func (process *Process) wait() {
	waitErr := process.cmd.Wait()
	finishedAt := time.Now().UTC()
	exitCode := -1
	if process.cmd.ProcessState != nil {
		exitCode = process.cmd.ProcessState.ExitCode()
	}

	process.mu.Lock()
	process.finished = true
	result := Result{
		PID: process.cmd.Process.Pid, ExitCode: exitCode,
		StartedAt: process.start, FinishedAt: finishedAt,
		Cancelled: process.cancel != nil,
	}
	process.result = result
	switch {
	case process.cancel != nil:
		process.err = process.cancel
	case waitErr != nil:
		process.err = &ExitError{Executable: process.spec.Executable, ExitCode: exitCode, Cause: waitErr}
	}
	process.mu.Unlock()
	// A successfully exited parent must not leave background descendants.
	_ = process.tree.Kill()
	_ = process.tree.Close()
	close(process.done)
}

func validateSpec(spec Spec) error {
	if spec.Executable == "" {
		return errors.New("process executable is required")
	}
	if spec.GracePeriod < 0 {
		return errors.New("process grace period cannot be negative")
	}
	return nil
}

type emitter struct {
	mu       sync.Mutex
	sequence uint64
	sink     Sink
	filter   Filter
}

func (emitter *emitter) write(stream Stream, data []byte) {
	if len(data) == 0 || emitter.sink == nil {
		return
	}
	emitter.mu.Lock()
	defer emitter.mu.Unlock()
	copyOfData := append([]byte(nil), data...)
	if emitter.filter != nil {
		copyOfData = emitter.filter(stream, copyOfData)
		if len(copyOfData) == 0 {
			return
		}
	}
	emitter.sequence++
	emitter.sink(Output{
		Sequence: emitter.sequence,
		Stream:   stream,
		Data:     copyOfData,
		Time:     time.Now().UTC(),
	})
}

type streamWriter struct {
	stream  Stream
	emitter *emitter
}

func (writer streamWriter) Write(data []byte) (int, error) {
	writer.emitter.write(writer.stream, data)
	return len(data), nil
}
