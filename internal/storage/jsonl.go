package storage

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

var fileLocks sync.Map

func lockFor(path string) *sync.Mutex {
	abs, _ := filepath.Abs(path)
	lock, _ := fileLocks.LoadOrStore(abs, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func AppendJSONL(path string, value any) error {
	lock := lockFor(path)
	lock.Lock()
	defer lock.Unlock()
	if err := EnsureNoSymlinkParents(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := RejectSymlinkTarget(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, FileMode)
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
	return nil
}

func ReadJSONL[T any](path string) ([]T, error) {
	if err := EnsureNoSymlinkParents(path); err != nil {
		return nil, errStorage("unsafe_path", path, err)
	}
	if err := RejectSymlinkTarget(path); err != nil {
		return nil, errStorage("unsafe_path", path, err)
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errStorage("open", path, err)
	}
	defer f.Close()
	var out []T
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, errStorage("broken_json", path, err)
		}
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, errStorage("read", path, err)
	}
	return out, nil
}

func RewriteJSONL[T any](path string, values []T) error {
	lock := lockFor(path)
	lock.Lock()
	defer lock.Unlock()
	if err := EnsureNoSymlinkParents(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := RejectSymlinkTarget(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
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
	return os.Chmod(path, FileMode)
}

func TruncateJSONL(path string) error {
	lock := lockFor(path)
	lock.Lock()
	defer lock.Unlock()
	if err := EnsureNoSymlinkParents(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := RejectSymlinkTarget(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, FileMode)
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
