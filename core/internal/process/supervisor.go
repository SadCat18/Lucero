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
// package declares the facts it needs recorded and nothing else, and no DB type
// crosses the seam (T-62 froze it: void methods, no row handle). The implementation
// is injected in cmd/core by backend.
//
// Two sentences this block used to carry are gone, and the reason matters more than the
// text: they claimed internal/process "must not import platform/db or eventjournal" and
// cited "internal contract §0's direction" for it. §0 (specs/contracts-internal-v0.1.md:13-25)
// bars only db -> 业务包 and audit -> db, and never names this package - the ban was my
// inference wearing a clause's name. T-211 and §1.1 第 17 条's 落点与角色 put the lifecycle
// write cell, "一次观察落成一行 + 恰一条事件", in this package, so platform/db **is** imported
// here (lifecycle.go only); what stays true is the narrower rule: Recorder itself declares no DB type.
//
// These three calls are the lifecycle writes into **`process_instances`**, and they
// carry no transition number: §14's lifecycle rows do not advance transition_no, so the
// injector must not try to allocate one here.
//
// Re-measured between 05:22:46 and 05:26:07 (`date` 实读的两个端点; the 13:34:37 reading
// this paragraph carried is void): `AggProcess`/`AggCore` exist (touch.go:42-43) and sit in
// `versionlessAgg` = {process: "process_instances", core: ""} while staying out of `aggTable`, exactly as
// §1.1 第 17 条 rules. So CO-39's question is answered the opposite way from what I asked:
// `process` **is** an AggregateKind now - it just has no version column, and `Append` is not
// the exit for this table's three facts (第 12 条's "除…外" exclusion + 第 17 条's ① entry).
// That reading aged inside the same round, so here is the 05:37:20 one: 第 17 条's ①
// `CreateVersionless` landed at 05:35:54 (touch.go:401) and all three entries match the
// contract's signatures 3/3 ⇒ the create leg has a door, and both open questions were ruled
// (T-221 / 契约① §1.1 第 20 条): CO-45 went to "one row per attempt" (id minted here, attempt
// and previous instance id in metadata_json), CO-44 to "0 rows moves nothing, books nothing,
// and is never silent". The door is now walked through by LifecycleStore in lifecycle.go,
// which is the only file in this package allowed to import platform/db
// (TestOnlyTheWriteCellTouchesThePlatform enforces the boundary the old comment only claimed).
//
// What an Event does NOT carry is the other half of the row: 000001's process_instances
// also requires id, restart_policy and (for an owned process) owner_type/owner_id, plus
// executable, created_at and updated_at. Those are assembly-time facts, not per-event
// ones, so the injector - built next to the Supervisor it supervises - is the only place
// that knows them. It hands them to this package's write cell once per Supervisor and does
// not stamp the row itself (T-211: 落库语义住在 internal/process; cmd/core 只做装配).
// What stays frozen is the seam in front of that cell: four void methods, no row handle
// (T-62 froze three, T-129 added the fourth), and TestEnumsFitTheFrozenProcessInstancesTable
// is the trip wire that the columns the cell will rely on still exist.
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
	// No row handle crosses this way: the injector knows which lifecycle fact a call is
	// from the event's Kind plus which row it belongs to from "the latest row for this
	// Supervisor", and a Supervisor is configured per process, so both halves are in
	// hand before the call. The halves are named after the columns they map to
	// (T-155): 000001's process_instances has process_type CHECK(ADAPTER/WORKER) plus
	// owner_type/owner_id - there is no process_role column, so an earlier draft of this
	// comment named a thing that does not exist.
	RecordStart(ev Event)
	// RecordExit is a clean exit: ev.Status is §14's EXITED, ev.ExitCode the real one.
	RecordExit(ev Event)
	// RecordCrash is an abnormal one, with ev.Crashes counting toward §8's budget.
	RecordCrash(ev Event)
	// RecordDrop is T84-7's half of §4.2 裁定 ①'s "丢弃 + Audit": the pump decided a frame
	// could not be attributed, and that decision has to leave a trace. It is the fourth
	// method, added on 主控's instruction in T-129 - T-62 froze the interface at three
	// methods and said not to reopen its shape, so the conflict is recorded rather than
	// ignored; the frozen properties are kept: void return, no row handle, no DB import.
	//
	// The distinction that matters: a drop is an audit fact, not a process lifecycle row
	// and not a domain event. The implementation must go through audit's own append API -
	// if this package ever wrote the audit_entries table itself it would be W13-2 red,
	// and the event journal must stay out of it (T84-7: 一次丢弃留一行审计、且不写
	// event_journal).
	RecordDrop(ev DroppedEvent)
}

// DroppedEvent is what the pump knows at the moment it throws a frame away: enough to
// reconstruct the decision afterwards, and nothing that guesses at attribution. The
// frame's own execution_id is reported as it arrived, not as the ticket was bound,
// precisely because the disagreement between the two is the event being recorded.
type DroppedEvent struct {
	Kind   Kind
	Method string
	// TurnRef is the ticket as it arrived, FrameExecutionID the execution the frame
	// itself named, BoundExecutionID what this connection had bound to that ticket.
	// The two execution ids are recorded unreconciled (T84-7): the disagreement between
	// them is the fact the audit row exists to preserve.
	TurnRef          string
	FrameExecutionID string
	BoundExecutionID string
	Reason           string
	Timestamp        int64
	// No role field: T-155 measured this file's ProcessRole as zero-producer,
	// zero-consumer and deleted it rather than inventing a producer for a column
	// 000001's process_instances does not have. contract ③'s field list still names it,
	// which is a contract-side lag for 主控 to close, not a reason to re-add the field.
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

// recordDrop hands one discarded frame to the injector. It is nil-safe for the same
// reason emit is: today nothing in the tree implements Recorder at all (T-129 measured
// it), so a drop must stay observable through the Inbound error and must never be the
// thing that panics a healthy connection.
func (s *Supervisor) recordDrop(ev DroppedEvent) {
	// s itself may be nil: a Connection built without a Supervisor (the pipe harness in
	// the tests) still has to be able to drop a frame. My first version guarded only Rec
	// and panicked the pump - a drop must never be the thing that kills a healthy
	// connection, which is the sentence this function's comment already carried.
	if s == nil || s.Rec == nil {
		return
	}
	if ev.Timestamp == 0 && s.Now != nil {
		ev.Timestamp = s.Now().UnixMilli()
	}
	s.Rec.RecordDrop(ev)
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
