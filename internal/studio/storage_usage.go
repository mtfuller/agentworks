package studio

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func runtimeStorageUsage(logRoot string) (int64, bool, error) {
	if strings.TrimSpace(logRoot) == "" {
		return 0, false, nil
	}
	root := filepath.Dir(filepath.Clean(logRoot))
	var bytes int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == root {
				return nil
			}
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		return nil
	})
	return bytes, true, err
}
