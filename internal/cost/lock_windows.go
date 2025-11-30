//go:build windows

package cost

import (
	"os"
	"time"
)

// acquireLock gets an exclusive lock using a .lock file presence
// Windows doesn't have flock, so we use file creation as a mutex
func acquireLock(lockFile string) (*os.File, error) {
	for i := 0; i < 10; i++ {
		// Try to create lock file exclusively
		f, err := os.OpenFile(lockFile, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
		if err == nil {
			return f, nil
		}

		// Check if lock file is stale (older than 30 seconds)
		if info, statErr := os.Stat(lockFile); statErr == nil {
			if time.Since(info.ModTime()) > 30*time.Second {
				os.Remove(lockFile)
				continue
			}
		}

		time.Sleep(50 * time.Millisecond)
	}

	// Give up and return nil - we'll proceed without lock
	return nil, os.ErrExist
}

// releaseLock releases the file lock by removing it
func releaseLock(f *os.File) {
	if f != nil {
		name := f.Name()
		f.Close()
		os.Remove(name)
	}
}
