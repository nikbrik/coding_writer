package storage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

var fileLocks sync.Map

const fileLockTimeout = 5 * time.Second

func lockFor(path string) *sync.Mutex {
	abs, _ := filepath.Abs(path)
	lock, _ := fileLocks.LoadOrStore(abs, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func AppendJSONL(path string, value any) error {
	if err := withJSONLLock(path, true, func() error {
		_, statErr := os.Stat(path)
		created := errors.Is(statErr, os.ErrNotExist)
		f, err := openFileNoFollow(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, FileMode)
		if err != nil {
			return errStorage("append", path, err)
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		if err := enc.Encode(value); err != nil {
			return errStorage("encode", path, err)
		}
		if err := f.Chmod(FileMode); err != nil {
			return errStorage("chmod", path, err)
		}
		if err := f.Sync(); err != nil {
			return errStorage("sync", path, err)
		}
		if created {
			if err := syncDir(filepath.Dir(path)); err != nil {
				return errStorage("sync_dir", filepath.Dir(path), err)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func UpdateJSONL[T any](path string, update func([]T) ([]T, error)) error {
	return withJSONLLock(path, true, func() error {
		values, err := readJSONLUnlocked[T](path)
		if err != nil {
			return err
		}
		updated, err := update(values)
		if err != nil {
			return err
		}
		return rewriteJSONLUnlocked(path, updated)
	})
}

func withJSONLLock(path string, createDir bool, fn func() error) error {
	return WithFileLock(path, createDir, fn)
}

func WithFileLock(path string, createDir bool, fn func() error) error {
	if err := EnsureNoSymlinkParents(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := RejectSymlinkTarget(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if createDir {
		if err := EnsureDir(filepath.Dir(path)); err != nil {
			return err
		}
	} else if _, err := os.Stat(filepath.Dir(path)); err != nil {
		if os.IsNotExist(err) {
			return fn()
		}
		return errStorage("stat_dir", filepath.Dir(path), err)
	}
	lock := lockFor(path)
	lock.Lock()
	defer lock.Unlock()
	unlock, err := acquireFileLock(path)
	if err != nil {
		var storageErr *Error
		if errors.As(err, &storageErr) {
			return err
		}
		return errStorage("lock", path, err)
	}
	defer unlock()
	return fn()
}

func readJSONLUnlocked[T any](path string) ([]T, error) {
	f, err := openFileNoFollow(path, os.O_RDONLY, FileMode)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errStorage("open", path, err)
	}
	defer f.Close()
	reader := bufio.NewReader(f)
	var out []T
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, errStorage("read", path, err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}
		var item T
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, errStorage("broken_json", path, err)
		}
		out = append(out, item)
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return out, nil
}

func ReadJSONL[T any](path string) ([]T, error) {
	var out []T
	err := withJSONLLock(path, false, func() error {
		values, err := readJSONLUnlocked[T](path)
		out = values
		return err
	})
	return out, err
}

func RewriteJSONL[T any](path string, values []T) error {
	return withJSONLLock(path, true, func() error {
		return rewriteJSONLUnlocked(path, values)
	})
}

func rewriteJSONLUnlocked[T any](path string, values []T) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.jsonl")
	if err != nil {
		return errStorage("temp", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	enc := json.NewEncoder(tmp)
	for _, value := range values {
		if err := enc.Encode(value); err != nil {
			_ = tmp.Close()
			return errStorage("encode", path, err)
		}
	}
	if err := tmp.Chmod(FileMode); err != nil {
		_ = tmp.Close()
		return errStorage("chmod", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return errStorage("sync", path, err)
	}
	if err := tmp.Close(); err != nil {
		return errStorage("close", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return errStorage("rename", path, err)
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return errStorage("sync_dir", filepath.Dir(path), err)
	}
	return os.Chmod(path, FileMode)
}

func TruncateJSONL(path string) error {
	if err := EnsureNoSymlinkParents(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := RejectSymlinkTarget(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	lock := lockFor(path)
	lock.Lock()
	defer lock.Unlock()
	unlock, err := acquireFileLock(path)
	if err != nil {
		return errStorage("lock", path, err)
	}
	defer unlock()
	f, err := openFileNoFollow(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, FileMode)
	if err != nil {
		return errStorage("truncate", path, err)
	}
	defer f.Close()
	if err := f.Chmod(FileMode); err != nil {
		return errStorage("chmod", path, err)
	}
	if err := f.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return errStorage("sync", path, err)
	}
	return nil
}

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

func openFileNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag|syscall.O_NOFOLLOW, perm)
}
