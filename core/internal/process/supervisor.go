// Package process is the Process Supervisor every external process must belong to
// (架构文档 §3.4（交接包）: "任何外部进程必须归属 Process Supervisor"). It owns launching,
// the stdout/stderr split, exit observation, the §8 restart backoff and the point at
// which an Adapter stops being retried.
//
// It deliberately knows nothing about the protocol: the caller gets the pipes and
// runs internal/adapter over them. It also writes no database rows and emits no
// events - contract §0 and the CO-3 ruling keep those behind injected functions, and
// §14's process_instances state writes belong to whoever holds the DB handle.
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

// Kind and Status mirror 000001's process_instances CHECK enumerations so a caller
// can register what it observes without inventing a third vocabulary. 000001 has no
// "state" column and no EXITING value; the transitional write is STOPPING.
type Kind string

const (
	KindAdapter Kind = "ADAPTER"
	KindWorker  Kind = "WORKER"
)

type Status string

const (
	StatusStarting Status = "STARTING"
	StatusRunning  Status = "RUNNING"
	StatusStopping Status = "STOPPING"
	StatusExited   Status = "EXITED"
	StatusFailed   Status = "FAILED"
	StatusUnknown  Status = "UNKNOWN"
)

// RestartPolicy is 000001's restart_policy enumeration.
type RestartPolicy string

const (
	RestartNever     RestartPolicy = "NEVER"
	RestartOnFailure RestartPolicy = "ON_FAILURE"
	RestartAlways    RestartPolicy = "ALWAYS"
)

// MaxConsecutiveCrashes is §8's budget: past it the Adapter goes UNAVAILABLE and
// automatic restarting stops.
const MaxConsecutiveCrashes = 5

// BackoffFor is §8's restart schedule: 1s, 2s, 4s, 8s, capped at 30s. The attempt
// argument counts consecutive failures, so a clean restart resets it.
func BackoffFor(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	// shift 5 gives 32s, which the cap below turns into 30s; §8 lists 1,2,4,8 then 30.
	d := time.Second << min(attempt-1, 5)
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

// EventKind names what Supervisor reports.
type EventKind string

const (
	EventStarted     EventKind = "STARTED"
	EventExited      EventKind = "EXITED"
	EventCrashed     EventKind = "CRASHED"
	EventUnavailable EventKind = "UNAVAILABLE"
)

// Event is one observation about the supervised process. Correlation IDs and DB rows
// are the caller's business; the counts are here because §8's rule is about them.
type Event struct {
	Kind      EventKind
	PID       int
	Status    Status
	ExitCode  int
	Crashes   int // consecutive crashes at this point
	Err       error
	Timestamp int64
}

// Child is one launched process plus its separated streams. Stdout is the protocol
// channel (§1: frames only) and Stderr is diagnostics Core archives as a blob.
type Child struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser
	PID    int

	cmd    *exec.Cmd
	done   chan error
	once   sync.Once
	waitMu sync.Mutex
	waited bool
	code   int
	werr   error
}

// Wait blocks until the child exits and reports its exit code. It is repeatable: the
// result is remembered, because Stop waits and a caller that then wants to know how
// the child died must not hang on a drained channel.
func (c *Child) Wait(ctx context.Context) (int, error) {
	c.waitMu.Lock()
	defer c.waitMu.Unlock()
	if c.waited {
		return c.code, c.werr
	}
	select {
	case err := <-c.done:
		c.code, c.werr = exitCode(err), nil
		if err != nil && c.code < 0 {
			c.code, c.werr = -1, err
		}
		c.waited = true
		return c.code, c.werr
	case <-ctx.Done():
		return -1, ctx.Err()
	}
}

// exitCode turns exec's error into the process status: -1 when there is none to read.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// CloseStreams releases the pipes once the caller is done with a live child.
func (c *Child) CloseStreams() {
	c.once.Do(func() {
		_ = c.Stdin.Close()
		_ = c.Stdout.Close()
		_ = c.Stderr.Close()
	})
}

// Launcher builds a fresh *exec.Cmd. A cmd cannot be re-Start()ed, so the Supervisor
// calls it again for every attempt; tests use it to re-exec themselves as the child.
type Launcher func() (*exec.Cmd, error)

// Supervisor runs one child, restarting it per §8 until the crash budget is spent.
//
// Recorder is the whole internal/process -> persistence surface, per T-55: this
// package declares the facts it needs recorded and nothing else. The implementation
// is injected in cmd/core by backend, and internal/process must not import
// platform/db or eventjournal (internal contract §0's direction), so the row shapes
// stay the injector's business - three methods is the entire vocabulary.
//
// These three calls are the lifecycle writes into **`process_instances`**, and they
// carry no transition number: §14's lifecycle rows do not advance transition_no, so the
// injector must not try to allocate one here.
//
// Measured against platform/db rather than from memory (13:34:37, `date` 实读): `process_instances` is
// **not** in `aggTable` and there is **no** `process` AggregateKind in `touch.go`'s
// registry, so `Touch`/`Create` cannot express this write - which is what internal
// contract §12 means when it lists the table among those whose three facts
// (`process_started`/`_exited`/`_crashed`, event contract's aggregate column reads
// `process`) ride on `Append` instead. Note §12's wording says "已由 `process` 聚合表达"
// while the registry has no such kind: the string is an event label here, not a versioned
// aggregate, and CO-39 asks 主控 to say so in the contract before someone adds AggProcess
// and starts versioning a table that has no version column.
//
// What an Event does NOT carry is the other half of the row: 000001's process_instances
// also requires id, restart_policy and (for an owned process) owner_type/owner_id, plus
// executable, created_at and updated_at. Those are assembly-time facts, not per-event
// ones - the injector is built next to the Supervisor it supervises, so it holds the
// policy and the owner and stamps the row with them. That is what keeps this interface
// three void methods with no row handle (T-62), and TestEnumsFitTheFrozenProcessInstancesTable
// is the trip wire that the columns it relies on still exist.
//
// Event.Crashes has no column to land in (000001 is frozen and never grew one), so the
// running count rides in metadata_json. Losing it would be silent, and §8's budget is
// the one number Recovery cannot recompute after Core itself restarts.
//
// Sink and Recorder are deliberately two channels: Sink is observation (logs, the §8
// Notification when the restart budget runs out), Recorder is intent to write a row.
// Nothing here waits for either, and neither may block the restart loop.
type Recorder interface {
	// RecordStart fires once per live process instance and carries the pid that
	// §14's process_instances row has to name.
	// No row handle crosses this way: (kind, process_role) plus "the latest row" plus the
	// in-memory crash counter already say which lifecycle fact a call is, and the
	// injector is configured per Supervisor, so it knows both halves of the key.
	RecordStart(ev Event)
	// RecordExit is a clean exit: ev.Status is §14's EXITED, ev.ExitCode the real one.
	RecordExit(ev Event)
	// RecordCrash is an abnormal one, with ev.Crashes counting toward §8's budget.
	RecordCrash(ev Event)
}

// Sink receives every Event; it must not block (the restart loop is on this
// goroutine). Backoff is a field rather than a constant call so a test can run the
// whole crash ladder without sleeping for 15 seconds.
type Supervisor struct {
	Kind    Kind
	Launch  Launcher
	Restart RestartPolicy
	Sink    func(Event)
	// Rec is the §14/T-55 write channel, and it may be nil: a Supervisor with no
	// recorder keeps supervising and simply reports through Sink, which is what lets
	// this package stay free of any persistence dependency.
	Rec     Recorder
	Backoff func(attempt int) time.Duration
	// Now feeds timestamps into events; the contract wants Unix ms and tests want
	// determinism.
	Now func() time.Time
	// MaxCrashes overrides MaxConsecutiveCrashes when non-zero; it exists so a test
	// can watch the ladder terminate without five real children.
	MaxCrashes int
}

// ErrUnavailable means the §8 budget is spent and the child will not be restarted.
var ErrUnavailable = errors.New("adapter is UNAVAILABLE: restart budget exhausted")

func (s *Supervisor) max() int {
	if s.MaxCrashes > 0 {
		return s.MaxCrashes
	}
	return MaxConsecutiveCrashes
}

func (s *Supervisor) emit(ev Event) {
	if ev.Timestamp == 0 && s.Now != nil {
		ev.Timestamp = s.Now().UnixMilli()
	}
	if s.Sink != nil {
		s.Sink(ev)
	}
	if s.Rec == nil {
		return
	}
	// The three methods are the whole interface, so an unlisted kind is not silently
	// dropped: UNAVAILABLE is §8's Notification, not a process lifecycle row.
	switch ev.Kind {
	case EventStarted:
		s.Rec.RecordStart(ev)
	case EventExited:
		// Run() emits an exited event after a crash to say "supervision ended", which
		// is not the same fact as "the process exited cleanly" - recording both would
		// write a clean-exit row over §14's last_error.
		if ev.Crashes == 0 {
			s.Rec.RecordExit(ev)
		}
	case EventCrashed:
		s.Rec.RecordCrash(ev)
	}
}

func (s *Supervisor) backoff(attempt int) time.Duration {
	if s.Backoff != nil {
		return s.Backoff(attempt)
	}
	return BackoffFor(attempt)
}

// Run supervises one child until ctx is cancelled, the child exits and is not
// restarted, or the crash budget is spent. It returns the last exit status observed.
//
// The loop is single-goroutine-per-supervised-process (no bare goroutines, 交接包 §9.2 rule
// 5): the child's Wait result is delivered on Child.done by Start.
func (s *Supervisor) Run(ctx context.Context) error {
	crashes := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		child, err := s.Start(ctx)
		if err != nil {
			return err
		}
		code, err := child.Wait(ctx)
		child.CloseStreams()
		if err != nil {
			return fmt.Errorf("wait for pid %d: %w", child.PID, err)
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			// A shutdown we asked for is not a crash: §14 says a graceful stop writes
			// neither last_error nor shutdown_reason.
			s.emit(Event{Kind: EventExited, PID: child.PID, Status: StatusExited, ExitCode: code})
			return nil
		}
		clean := code == 0
		if !clean {
			crashes++
			s.emit(Event{Kind: EventCrashed, PID: child.PID, Status: StatusFailed,
				ExitCode: code, Crashes: crashes})
		}
		if s.Restart == RestartNever || (s.Restart == RestartOnFailure && clean) {
			s.emit(Event{Kind: EventExited, PID: child.PID, Status: StatusExited,
				ExitCode: code, Crashes: crashes})
			return nil
		}
		if crashes >= s.max() {
			// §8: health=UNAVAILABLE, stop auto-restarting, and let the caller raise a
			// Notification plus the adapter_restart_exhausted event.
			s.emit(Event{Kind: EventUnavailable, PID: child.PID, Status: StatusFailed,
				ExitCode: code, Crashes: crashes})
			return fmt.Errorf("%w after %d consecutive crashes (exit %d)", ErrUnavailable, crashes, code)
		}
		delay := s.backoff(crashes)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// Start launches one attempt and hands back the child with its streams split. The
// caller owns Stdout: feeding it to adapter.Reader is the protocol layer's job, not
// this one's.
func (s *Supervisor) Start(ctx context.Context) (*Child, error) {
	cmd, err := s.Launch()
	if err != nil {
		return nil, fmt.Errorf("build command: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	c := &Child{Stdin: stdin, Stdout: stdout, Stderr: stderr, PID: cmd.Process.Pid,
		cmd: cmd, done: make(chan error, 1)}
	go func() { c.done <- cmd.Wait() }() // one goroutine per child, tied to its lifetime
	s.emit(Event{Kind: EventStarted, PID: c.PID, Status: StatusStarting})
	return c, nil
}

// Stop asks the live child to shut down and marks the transitional status. §14's
// ruling is that the DB row must not stay RUNNING across this window, so the caller
// writes STOPPING through the injected write function before calling Stop.
func (s *Supervisor) Stop(c *Child, timeout time.Duration) error {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	// Kill rather than Signal: Windows has no SIGTERM, and a graceful stop is the
	// caller's job (send $/cancel, close stdin) before it gets here.
	if err := c.cmd.Process.Kill(); err != nil {
		return fmt.Errorf("kill: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := c.Wait(ctx)
	return err
}
