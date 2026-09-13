package recordpackage

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	packageDirPerm  = 0o755
	packageFilePerm = 0o644
)

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, packageDirPerm); err != nil {
		return fmt.Errorf("create parent directory for %q: %w", path, err)
	}

	tmpFile, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %q: %w", path, err)
	}
	tmpPath := tmpFile.Name()

	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temporary file for %q: %w", path, err)
	}
	if err := tmpFile.Chmod(packageFilePerm); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("set permissions on temporary file for %q: %w", path, err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temporary file for %q: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("finalize file %q: %w", path, err)
	}

	cleanup = false
	return nil
}

func ensureDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(path, packageDirPerm)
		}
		return fmt.Errorf("stat directory %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a directory", ErrPackageDestinationExists, path)
	}
	return nil
}
