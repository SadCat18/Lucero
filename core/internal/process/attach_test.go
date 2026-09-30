package process

// Tests for T-398 ①: the exported "connection over the supervised child" entry, and the
// one-process-per-Supervisor rule that goes with it. Every claim here is about a launch
// *count* read from the Launcher, because that is the only way to answer "did the second
// path start a process?" without trusting a pid that may already have been recycled.

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SadCat18/lucero/core/internal/adapter"
)

// countedLauncher wraps any Launcher and remembers how many processes it actually built.
type countedLauncher struct {
	mu      sync.Mutex
	started int
	inner   Launcher
}

func countLaunches(inner Launcher) *countedLauncher { return &countedLauncher{inner: inner} }

func (c *countedLauncher) Launch() (*exec.Cmd, error) {
	cmd, err := c.inner()
	if err == nil {
		c.mu.Lock()
		c.started++
		c.mu.Unlock()
	}
	return cmd, err
}

func (c *countedLauncher) starts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.started
}

// attached reports whether a child still carries a connection, read under the same mutex
// the entry paths use. A test that only exercised the round trip would pass even if the
// claim were never recorded, and the claim is what Run reads.
func attached(s *Supervisor, c *Child) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return c.conn != nil
}

// TestNewConnectionRidesTheChildWithoutStartingAnother is the point of the entry: one
// Start, one process, and a real handshake plus a round trip over exactly that pid.
func TestNewConnectionRidesTheChildWithoutStartingAnother(t *testing.T) {
	exe := buildFakeAdapter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	launch := countLaunches(func() (*exec.Cmd, error) { return exec.CommandContext(ctx, exe), nil })
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: launch.Launch}

	child, err := sup.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	conn, err := sup.NewConnection(child, "0.1.0-test")
	if err != nil {
		t.Fatalf("NewConnection: %v", err)
	}
	if got := launch.starts(); got != 1 {
		t.Errorf("launches after NewConnection = %d, want 1: the entry exists so no second process is started", got)
	}
	if conn.Child().PID != child.PID {
		t.Errorf("connection rides pid %d, Start handed back %d", conn.Child().PID, child.PID)
	}
	if !attached(sup, child) {
		t.Error("NewConnection did not record its claim on the child, so Run's guard has nothing to read")
	}
	if _, err := conn.Initialize(ctx); err != nil {
		t.Fatalf("Initialize over the supervised child: %v", err)
	}
	if _, err := conn.Call(ctx, adapter.MethodHealth, struct{}{}); err != nil {
		t.Errorf("adapter/health over the supervised child: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if attached(sup, child) {
		t.Error("Close left the claim on the child: the Supervisor would be locked behind a finished connection")
	}
}

// TestNewConnectionRefusesAChildItDidNotStart pins the check rather than the convenience:
// the entry is "from *this* Supervisor's child" because the crash budget, the §14 row and
// the pipes must all describe one process somebody owns. The refusal must also cost
// nothing - no second process, and the real owner keeps its child.
func TestNewConnectionRefusesAChildItDidNotStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ownerLaunch := countLaunches(helperLauncher("live"))
	owner := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: ownerLaunch.Launch}
	child, err := owner.Start(ctx)
	if err != nil {
		t.Fatalf("owner Start: %v", err)
	}
	defer func() { _ = owner.Stop(child, 2*time.Second); child.CloseStreams() }()

	strangerLaunch := countLaunches(helperLauncher("live"))
	stranger := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: strangerLaunch.Launch}
	if _, err := stranger.NewConnection(child, "0.1.0-test"); !errors.Is(err, ErrForeignChild) {
		t.Fatalf("a stranger Supervisor attaching pid %d = %v, want ErrForeignChild", child.PID, err)
	}
	if got := strangerLaunch.starts(); got != 0 {
		t.Errorf("the refusal started %d processes, want 0", got)
	}
	if attached(stranger, child) {
		t.Error("the refused attach recorded a claim on the child for the Supervisor that was refused")
	}
	conn, err := owner.NewConnection(child, "0.1.0-test")
	if err != nil {
		t.Fatalf("the real owner was refused after the failed attach: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestOneChildCarriesOneConnection is the same stdout pipe from the other side: a second
// pump over one child would split the frame stream between two readers, which is not a
// quieter version of the right thing but a corrupted view of it.
func TestOneChildCarriesOneConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	launch := countLaunches(helperLauncher("live"))
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: launch.Launch}
	child, err := sup.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	first, err := sup.NewConnection(child, "0.1.0-test")
	if err != nil {
		t.Fatalf("first NewConnection: %v", err)
	}
	if _, err := sup.NewConnection(child, "0.1.0-test"); !errors.Is(err, ErrChildBusy) {
		t.Errorf("second connection over pid %d = %v, want ErrChildBusy", child.PID, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The reconnect shape cmd/core's pump uses: a fresh child, attached cleanly.
	next, err := sup.Start(ctx)
	if err != nil {
		t.Fatalf("Start after Close: %v", err)
	}
	second, err := sup.NewConnection(next, "0.1.0-test")
	if err != nil {
		t.Fatalf("NewConnection over the replacement child: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Errorf("Close of the replacement: %v", err)
	}
}

// TestRunRefusesToStartASecondProcessBesideAConnection is the line T-398 ① asked for: one
// Supervisor, one process. Run is the §8 ladder and it starts its own child by design, so
// beside a live connection it must say so and name whose process is in the way - not
// quietly put a second pid behind the same row, budget and health state.
func TestRunRefusesToStartASecondProcessBesideAConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	launch := countLaunches(helperLauncher("live"))
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: launch.Launch}
	child, err := sup.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	conn, err := sup.NewConnection(child, "0.1.0-test")
	if err != nil {
		t.Fatalf("NewConnection: %v", err)
	}
	runCtx, runCancel := context.WithTimeout(ctx, 3*time.Second)
	defer runCancel()

	err = sup.Run(runCtx)
	if !errors.Is(err, ErrChildBusy) {
		conn.Close()
		t.Fatalf("Run beside a live connection = %v, want ErrChildBusy", err)
	}
	if !strings.Contains(err.Error(), strconv.Itoa(child.PID)) {
		t.Errorf("the refusal does not name the pid holding the Supervisor: %v", err)
	}
	if got := launch.starts(); got != 1 {
		t.Errorf("launches = %d, want 1: Run must not start a second process for the same Supervisor", got)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestRunIsAllowedAgainOnceTheConnectionIsClosed is the 放行 half of the gate: a rule that
// only ever refuses would read as a deadlock, so the ladder has to come back for an adapter
// that was shut down properly.
//
// It is also the release's witness, and that took a measurement to earn: while Run's guard
// let a *reaped* child out of the refusal, dropping the release (mutations a4/a5) fired only
// the `attached()` readback in the first test and left this one green - the child was reaped,
// so the guard passed for an unrelated reason. The guard now ends at Close and nowhere else,
// which is what makes a stuck claim fail here too.
func TestRunIsAllowedAgainOnceTheConnectionIsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	launch := countLaunches(helperLauncher("clean"))
	sup := &Supervisor{Kind: KindAdapter, Restart: RestartNever, Launch: launch.Launch}
	child, err := sup.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	conn, err := sup.NewConnection(child, "0.1.0-test")
	if err != nil {
		t.Fatalf("NewConnection: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	runCtx, runCancel := context.WithTimeout(ctx, 20*time.Second)
	defer runCancel()
	if err := sup.Run(runCtx); err != nil {
		t.Fatalf("Run after Close = %v, want the ladder to run over a process of its own", err)
	}
	if got := launch.starts(); got != 2 {
		t.Errorf("launches = %d, want 2 (the connection's child, then Run's own)", got)
	}
}
