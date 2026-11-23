// Package synclock makes sure only one LedgerGuru process at a time sends sync requests to
// TallyPrime. Each AI app (and the control panel, the command line) runs its own copy of the
// server; a mutex inside one process cannot stop two copies from syncing at once, which would
// give Tally parallel requests. The lock is a file in the shared data folder, held with an OS
// file lock, so it is released by the OS even if the process ends without cleaning up.
package synclock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Name is the lock file in the data folder.
const Name = "sync.lock"

// Poll is how often a waiting process tries the lock again.
var Poll = 2 * time.Second

// errBusy is returned by tryLock when another process holds the lock.
var errBusy = errors.New("locked")

// Lock is a held lock; Unlock releases it.
type Lock struct{ f *os.File }

// Acquire takes the lock in dir, waiting while another process holds it. waiting is called
// once, the first time it has to wait. It gives up when ctx ends.
func Acquire(ctx context.Context, dir string, waiting func()) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, Name), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	told := false
	for {
		err := tryLock(f)
		if err == nil {
			return &Lock{f}, nil
		}
		if !errors.Is(err, errBusy) {
			f.Close()
			return nil, err
		}
		if !told && waiting != nil {
			waiting()
			told = true
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(Poll):
		}
	}
}

// Unlock releases the lock.
func (l *Lock) Unlock() {
	if l == nil || l.f == nil {
		return
	}
	unlock(l.f)
	l.f.Close()
	l.f = nil
}
