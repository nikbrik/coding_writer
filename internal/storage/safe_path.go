package storage

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// NOTE: Path safety below assumes Unix-like filesystems. It explicitly rejects
// backslashes in path elements and IDs, which is correct for IDs that become
// filenames but means the tool is not currently portable to Windows without
// replacing these checks with OS-specific validation (e.g. filepath.IsLocal).

var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func ValidateID(id string) error {
	decoded, _ := url.PathUnescape(id)
	if id == "" || decoded != id || !safeIDPattern.MatchString(id) || strings.ContainsAny(id, `/\\`) || strings.Contains(id, "..") {
		return errors.New("unsafe id")
	}
	return nil
}

func SafeJoin(root string, elems ...string) (string, error) {
	if root == "" {
		return "", errors.New("empty root")
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	parts := []string{cleanRoot}
	for _, elem := range elems {
		if elem == "" || filepath.IsAbs(elem) {
			return "", errors.New("unsafe path")
		}
		decoded, _ := url.PathUnescape(elem)
		if decoded != elem || strings.ContainsAny(elem, `\\`) {
			return "", errors.New("unsafe path")
		}
		for _, part := range strings.Split(elem, string(filepath.Separator)) {
			if part == "" || part == "." || part == ".." {
				return "", errors.New("unsafe path")
			}
		}
		parts = append(parts, elem)
	}
	joined := filepath.Clean(filepath.Join(parts...))
	rel, err := filepath.Rel(cleanRoot, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes root")
	}
	return joined, nil
}

func EnsureNoSymlinkParents(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	parts := strings.Split(filepath.Clean(dir), string(filepath.Separator))
	if filepath.IsAbs(dir) {
		parts[0] = string(filepath.Separator)
	}
	current := ""
	resolvedAbs := abs
	for _, part := range parts {
		if part == "" {
			continue
		}
		if part == string(filepath.Separator) {
			current = part
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(current)
			if err != nil {
				return err
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(current), target)
			}
			target = filepath.Clean(target)
			if resolvedAbs == current {
				resolvedAbs = target
			} else if strings.HasPrefix(resolvedAbs, current+string(filepath.Separator)) {
				resolvedAbs = target + resolvedAbs[len(current):]
			}
			if resolvedAbs == target || strings.HasPrefix(resolvedAbs, target+string(filepath.Separator)) {
				continue
			}
			return errors.New("symlink parent rejected")
		}
	}
	return nil
}

func RejectSymlinkTarget(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("symlink target rejected")
	}
	return nil
}
