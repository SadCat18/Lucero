package process

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestHelperProcess is not a test: re-executed with LUCERO_HELPER set, it *is* the
// child process. That keeps these tests about real OS processes - the whole point of
// §8 - without shelling out to the go toolchain to build a binary first.
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("LUCERO_HELPER") {
	case "":
		t.Skip("re-exec helper: set LUCERO_HELPER=crash|live|clean")
	case "crash":
		os.Exit(2) // what fake-adapter's debug/crash does (§10)
	case "clean":
		os.Exit(0)
	case "live":
		// Echo every line until the parent closes stdin. Never sleep here: a helper
		// that lingers holds the write end of the pipe open, so any reader on the
		// other side waits, and an unreaped one outlives the test that spawned it.
		io.WriteString(os.Stderr, "diagnostic on stderr\n")
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			io.WriteString(os.Stdout, "pong "+sc.Text()+"\n")
		}
		os.Exit(0)
	}
}

// helperLauncher re-execs this test binary as the child.
func helperLauncher(mode string) Launcher {
	return func() (*exec.Cmd, error) {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "-test.timeout=60s")
		cmd.Env = append(os.Environ(), "LUCERO_HELPER="+mode)
		return cmd, nil
	}
}

// stubRecorder is the shape backend will inject in cmd/core, reduced to remembering:
// the point of T-55's interface is that this package cannot reach a DB, so a test can
// only assert what was handed over, never what row it became.
type stubRecorder struct {
	started, exits, crashes []Event
	// rows stands for the row handle cmd/core owns: this package only ever sees the
	// string come back, then go out again untouched.
	rows int
}

func (r *stubRecorder) RecordStart(ev Event) { r.started = append(r.started, ev) }

func (r *stubRecorder) RecordExit(ev Event) { r.exits = append(r.exits, ev) }

func (r *stubRecorder) RecordCrash(ev Event) { r.crashes = append(r.crashes, ev) }

func TestRecorderSeesCleanExitOnce(t *testing.T) {
	rec := new(stubRecorder)
	s := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: helperLauncher("clean"), Rec: rec,
		Now: func() time.Time { return time.UnixMilli(1700000000000) }}
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.started) != 1 || rec.started[0].PID <= 0 {
		t.Errorf("RecordStart calls = %d, want one with a real pid", len(rec.started))
	}
	if len(rec.exits) != 1 || rec.exits[0].ExitCode != 0 {
		t.Errorf("RecordExit calls = %+v, want one clean exit", rec.exits)
	}
	if len(rec.crashes) != 0 {
		t.Errorf("RecordCrash fired for a clean exit: %+v", rec.crashes)
	}
	if got := rec.exits[0].Timestamp; got != 1700000000000 {
		t.Errorf("timestamp = %d, want Now()'s Unix ms (§14 stores ms)", got)
	}
}

func TestRecorderSeparatesCrashFromExit(t *testing.T) {
	rec := new(stubRecorder)
	var seen []EventKind
	s := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: helperLauncher("crash"), Rec: rec,
		Sink: func(ev Event) { seen = append(seen, ev.Kind) },
		Now:  func() time.Time { return time.UnixMilli(1700000000000) }}
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.crashes) != 1 || rec.crashes[0].ExitCode != 2 {
		t.Errorf("RecordCrash calls = %+v, want one with §10's exit code 2", rec.crashes)
	}
	// Run also emits an exited event to mean "supervision ended". Writing that as a
	// clean-exit row would overwrite §14's last_error, so the recorder must not see it.
	if len(rec.exits) != 0 {
		t.Errorf("a crash wrote %d exit rows, want 0", len(rec.exits))
	}
	// Observation keeps both, because Sink is where §8's notifications come from.
	if len(seen) != 3 || seen[0] != EventStarted || seen[1] != EventCrashed || seen[2] != EventExited {
		t.Errorf("Sink saw %v, want STARTED, CRASHED, EXITED", seen)
	}
}

func TestRecorderStaysSilentOnUnavailable(t *testing.T) {
	rec := new(stubRecorder)
	var seen []EventKind
	s := &Supervisor{Kind: KindAdapter, Restart: RestartOnFailure, MaxCrashes: 2,
		Launch: helperLauncher("crash"), Rec: rec, Backoff: func(int) time.Duration { return time.Millisecond },
		Sink: func(ev Event) { seen = append(seen, ev.Kind) }}
	if err := s.Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Run = %v, want ErrUnavailable", err)
	}
	if len(rec.crashes) != 2 {
		t.Errorf("RecordCrash calls = %d, want one per crash (2)", len(rec.crashes))
	}
	if rec.crashes[1].Crashes != 2 {
		t.Errorf("second crash carried Crashes=%d, want the running count for §8's budget", rec.crashes[1].Crashes)
	}
	if len(rec.exits) != 0 || len(rec.started) != 2 {
		t.Errorf("rows = %d exits / %d starts, want 0 exits and one start per launch", len(rec.exits), len(rec.started))
	}
	last := seen[len(seen)-1]
	if last != EventUnavailable {
		t.Fatalf("last observation = %s, want UNAVAILABLE", last)
	}
	// §8's exhaustion is a Notification, not a process lifecycle row.
	for _, ev := range rec.crashes {
		if ev.Kind != EventCrashed {
			t.Errorf("recorder got a %s row, want only lifecycle rows", ev.Kind)
		}
	}
}

func TestBackoffFor(t *testing.T) {
	// §8: 1s, 2s, 4s, 8s, 上限 30s. The reading taken here is "keep doubling, then
	// cap", so attempt 5 is 16s; CO-19 asks 主控 to confirm or pin 30s right after 8s.
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0}, {1, time.Second}, {2, 2 * time.Second}, {3, 4 * time.Second},
		{4, 8 * time.Second}, {5, 16 * time.Second}, {6, 30 * time.Second}, {99, 30 * time.Second},
	}
	for _, c := range cases {
		if got := BackoffFor(c.attempt); got != c.want {
			t.Errorf("BackoffFor(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func record(sink *[]Event) func(Event) {
	return func(e Event) { *sink = append(*sink, e) }
}

// TestSupervisorStopsAfterFiveCrashes is §8's budget: after MaxConsecutiveCrashes
// consecutive crashes the Adapter is UNAVAILABLE and no further attempt is made.
func TestSupervisorStopsAfterFiveCrashes(t *testing.T) {
	var events []Event
	launches := 0
	s := &Supervisor{
		Kind:    KindAdapter,
		Restart: RestartOnFailure,
		Launch: func() (*exec.Cmd, error) {
			launches++
			return helperLauncher("crash")()
		},
		Sink:    record(&events),
		Backoff: func(int) time.Duration { return time.Millisecond },
	}
	err := s.Run(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Run = %v, want ErrUnavailable", err)
	}
	if launches != MaxConsecutiveCrashes {
		t.Errorf("launched %d times, want exactly %d before giving up", launches, MaxConsecutiveCrashes)
	}
	var last Event
	for _, e := range events {
		last = e
	}
	if last.Kind != EventUnavailable || last.Crashes != MaxConsecutiveCrashes {
		t.Errorf("last event = %+v, want UNAVAILABLE at crash %d", last, MaxConsecutiveCrashes)
	}
	if last.Status != StatusFailed {
		t.Errorf("UNAVAILABLE event status = %s, want %s", last.Status, StatusFailed)
	}
}

// TestCleanExitIsNotACrash pins the ON_FAILURE half of §8: a child that exits 0 must
// not consume the crash budget.
func TestCleanExitIsNotACrash(t *testing.T) {
	launches, crashes := 0, 0
	s := &Supervisor{
		Restart: RestartOnFailure,
		Launch: func() (*exec.Cmd, error) {
			launches++
			return helperLauncher("clean")()
		},
		Backoff: func(int) time.Duration {
			crashes++
			return time.Millisecond
		},
	}
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("Run = %v, want a clean return", err)
	}
	if launches != 1 || crashes != 0 {
		t.Errorf("launches=%d restartWaits=%d, want 1 and 0", launches, crashes)
	}
}

// TestCrashedAdapterLeavesAnotherAlone is the §8 regression 测试角色 is blocked on, at
// the granularity this package can prove: one Adapter process dies repeatedly while
// the supervisor's own process (here, the test binary standing in for Core) stays
// alive and keeps a full conversation with a second Adapter.
//
// The Execution-to-UNKNOWN half is not here on purpose: that is a state value the
// execution owner writes, and this package only reports the crash.
func TestCrashedAdapterLeavesAnotherAlone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sup := &Supervisor{Kind: KindAdapter, Launch: helperLauncher("live")}
	healthy, err := sup.Start(ctx)
	if err != nil {
		t.Fatalf("start healthy adapter: %v", err)
	}
	t.Cleanup(func() { _ = sup.Stop(healthy, 5*time.Second) })
	defer healthy.CloseStreams()
	// One reader per stream: a fresh bufio.Reader can pull bytes of a previous one's
	// buffer into the bin.
	out, errs := bufio.NewReader(healthy.Stdout), bufio.NewReader(healthy.Stderr)
	send := func(s string) string {
		t.Helper()
		if _, err := healthy.Stdin.Write([]byte(s + "\n")); err != nil {
			t.Fatalf("write %q: %v", s, err)
		}
		line, err := out.ReadString('\n')
		if err != nil {
			t.Fatalf("read after %q: %v", s, err)
		}
		return line
	}

	if got := send("ping"); got != "pong ping\n" {
		t.Errorf("adapter answered %q, want %q", got, "pong ping\n")
	}

	// Let the other Adapter crash itself to death.
	var events []Event
	crasher := &Supervisor{Kind: KindAdapter, Restart: RestartOnFailure,
		Launch: helperLauncher("crash"), Sink: record(&events),
		Backoff: func(int) time.Duration { return time.Millisecond }}
	if err := crasher.Run(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("crashing adapter Run = %v, want ErrUnavailable", err)
	}

	// Core is still here, and the healthy Adapter still answers on the same pipes.
	if got := send("again"); got != "pong again\n" {
		t.Errorf("adapter answered %q after its sibling died", got)
	}
	if healthy.PID == crasherLastPID(events) {
		t.Error("the two adapters share a PID; the test would prove nothing")
	}
	// §1's stderr split: the diagnostic arrives on stderr and never inside a frame.
	diag, err := errs.ReadString('\n')
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if diag != "diagnostic on stderr\n" {
		t.Errorf("stderr = %q, want the child's diagnostic only", diag)
	}
}

func crasherLastPID(events []Event) int {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == EventStarted {
			return events[i].PID
		}
	}
	return 0
}

// TestStopKillsTheChild covers the §14 transitional window: after Stop the child is
// gone and the supervisor reports it, so the caller can write STOPPING and then the
// terminal status without leaving a RUNNING row behind.
func TestStopKillsTheChild(t *testing.T) {
	ctx := context.Background()
	child, err := (&Supervisor{Launch: helperLauncher("live")}).Start(ctx)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer child.CloseStreams()
	if _, err := child.Stdin.Write([]byte("x\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := bufio.NewReader(child.Stdout).ReadString('\n'); err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := (&Supervisor{}).Stop(child, 5*time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	code, err := child.Wait(ctx)
	if err != nil {
		t.Fatalf("wait after stop: %v", err)
	}
	if code == 0 {
		t.Errorf("exit code 0 after Kill, want a non-zero killed status")
	}
}
