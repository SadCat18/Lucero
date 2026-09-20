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
