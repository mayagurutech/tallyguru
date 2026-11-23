package synclock

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// The lock must hold across processes: a child process tries it while this one holds it.
func TestLockAcrossProcesses(t *testing.T) {
	if d := os.Getenv("SYNCLOCK_CHILD"); d != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		Poll = 50 * time.Millisecond
		l, err := Acquire(ctx, d, nil)
		if err == nil {
			l.Unlock()
			os.Exit(3) // got a lock that another process holds
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	l, err := Acquire(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestLockAcrossProcesses")
	cmd.Env = append(os.Environ(), "SYNCLOCK_CHILD="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("second process got the lock while it was held: %v %s", err, out)
	}
	l.Unlock()
	waited := false
	l2, err := Acquire(context.Background(), dir, func() { waited = true })
	if err != nil || waited {
		t.Fatalf("free lock: %v waited=%v", err, waited)
	}
	l2.Unlock()
}

func TestWaitsThenGets(t *testing.T) {
	Poll = 20 * time.Millisecond
	dir := t.TempDir()
	l, _ := Acquire(context.Background(), dir, nil)
	// Same process, separate file handle: Windows and flock both treat it as another holder.
	go func() { time.Sleep(100 * time.Millisecond); l.Unlock() }()
	waited := false
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l2, err := Acquire(ctx, dir, func() { waited = true })
	if err != nil || !waited {
		t.Fatalf("want wait then lock: %v waited=%v", err, waited)
	}
	l2.Unlock()
}
