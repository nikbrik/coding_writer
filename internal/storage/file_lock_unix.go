//go:build unix

package storage

import (
	"errors"
	"os"
	"syscall"
	"time"
)

func acquireFileLock(path string) (func(), error) {
	lockPath := path + ".lock"
	if err := RejectSymlinkTarget(lockPath); err != nil {
		return nil, err
	}
	f, err := openFileNoFollow(lockPath, os.O_CREATE|os.O_RDWR, FileMode)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(FileMode); err != nil {
		_ = f.Close()
		return nil, err
	}
	deadline := time.Now().Add(fileLockTimeout)
	for {
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			break
		} else if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, errStorage("lock_timeout", lockPath, errors.New("file lock timeout"))
		}
		time.Sleep(10 * time.Millisecond)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func requireSecureStoragePlatform(path string) error { return nil }

func openFileNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag|syscall.O_NOFOLLOW, perm)
}
