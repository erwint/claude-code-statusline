//go:build !windows

package cost

import (
	"os"
	"syscall"
	"time"
)

// acquireLock gets an exclusive lock on the lock file
func acquireLock(lockFile string) (*os.File, error) {
	f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	// Try to acquire exclusive lock with retries
	for i := 0; i < 10; i++ {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	f.Close()
	return nil, err
}

// releaseLock releases the file lock
func releaseLock(f *os.File) {
	if f != nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}
