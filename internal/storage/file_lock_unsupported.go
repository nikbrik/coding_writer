//go:build !unix

package storage

import (
	"errors"
	"os"
	"runtime"
)

func acquireFileLock(path string) (func(), error) {
	return nil, errStorage("unsupported_platform", path, errors.New("secure file locking is unsupported on "+runtime.GOOS))
}

func requireSecureStoragePlatform(path string) error {
	return errStorage("unsupported_platform", path, errors.New("secure file locking/no-follow storage is unsupported on "+runtime.GOOS))
}

func openFileNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	return nil, errStorage("unsupported_platform", path, errors.New("no-follow file open is unsupported on "+runtime.GOOS))
}
