package guiassets

import (
	"io/fs"
	"os"
	"path/filepath"
)

func FrontendFS() (fs.FS, error) {
	if envDir := os.Getenv("CARACAL_SETUP_FRONTEND_DIST"); envDir != "" {
		return validatedDirFS(envDir)
	}

	var candidates []string
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, candidateRelativePaths(wd, filepath.Join("frontend", "dist"))...)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, candidateRelativePaths(filepath.Dir(exe), filepath.Join("frontend", "dist"))...)
	}

	seen := make(map[string]struct{})
	for _, dir := range candidates {
		clean := filepath.Clean(dir)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}

		found, err := validatedDirFS(clean)
		if err == nil {
			return found, nil
		}
	}

	return nil, fs.ErrNotExist
}

func validatedDirFS(dir string) (fs.FS, error) {
	indexPath := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return nil, err
	}
	return os.DirFS(dir), nil
}

func candidateRelativePaths(start string, relative string) []string {
	var paths []string
	for dir := filepath.Clean(start); ; dir = filepath.Dir(dir) {
		paths = append(paths, filepath.Join(dir, relative))
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return paths
}
