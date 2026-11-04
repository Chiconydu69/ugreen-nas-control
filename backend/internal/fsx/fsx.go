// Package fsx writes the plugin's state files safely: a file is written to a temporary name in the same folder
// and renamed over the target, and nothing is created through a symbolic link. A crash never leaves half a file, and
// a link planted at a predictable name in /var/run or /boot/config cannot redirect a root write.
package fsx

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// WriteFile replaces path with data (like os.WriteFile, but atomic). A symbolic link at path is replaced, not followed.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	sweepStale(filepath.Dir(path), filepath.Base(path))
	// CreateTemp: O_EXCL, a name no other goroutine or process shares, never through a link. It creates 0600.
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	fail := func(err error) error {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Chmod(perm); err != nil {
		return fail(err)
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	// On the flash behind /boot a rename can reach the disk before the data (FAT, power cut).
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Make the rename itself durable; FAT and some filesystems cannot sync a directory, which is no failure.
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// staleTemp is the age after which a temporary file of an earlier write counts as left by a crash.
const staleTemp = time.Minute

// sweepStale removes ".base.tmp*" files older than staleTemp: a crash between CreateTemp and rename would
// otherwise leave one behind per crash on the flash behind /boot. Errors are ignored; the write goes on.
func sweepStale(dir, base string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := "." + base + ".tmp"
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), prefix) || !e.Type().IsRegular() {
			continue
		}
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > staleTemp {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// OpenTrunc opens path for writing, emptied (a log that is rewritten); it fails on a symbolic link.
func OpenTrunc(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
}

// OpenAppend opens path for appending; it fails on a symbolic link.
func OpenAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND|syscall.O_NOFOLLOW, 0o644)
}

// OpenLock opens a lock file (flock); it fails on a symbolic link.
func OpenLock(path string, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, perm)
}
