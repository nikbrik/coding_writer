package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	DirMode  os.FileMode = 0o700
	FileMode os.FileMode = 0o600
)

func EnsureDir(path string) error {
	if err := EnsureNoSymlinkParents(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := RejectSymlinkTarget(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := os.MkdirAll(path, DirMode); err != nil {
		return errStorage("mkdir", path, err)
	}
	if err := RejectSymlinkTarget(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	return os.Chmod(path, DirMode)
}

func AtomicWriteJSON(path string, value any) error {
	if err := EnsureNoSymlinkParents(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := RejectSymlinkTarget(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.json")
	if err != nil {
		return errStorage("temp", path, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		_ = tmp.Close()
		return errStorage("encode", path, err)
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

func ReadJSON(path string, value any) error {
	if err := EnsureNoSymlinkParents(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	if err := RejectSymlinkTarget(path); err != nil {
		return errStorage("unsafe_path", path, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return errStorage("open", path, err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return errStorage("broken_json", path, err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return errStorage("broken_json", path, errors.New("trailing data"))
	}
	return nil
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		if errors.Is(err, os.ErrInvalid) {
			return nil
		}
		return fmt.Errorf("fsync dir: %w", err)
	}
	return nil
}
