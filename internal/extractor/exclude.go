package extractor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

func excludeMatch(gi *ignore.GitIgnore, rel string, isDir bool) bool {
	if isDir {
		return gi.MatchesPath(rel) || gi.MatchesPath(rel+"/")
	}
	return gi.MatchesPath(rel)
}

// prepareExcludeSpec: DefaultGlobs -> .gitignore (-g, dir root) -> -x.
// Dedupes preserving first occurrence. Blank/# lines are no-ops.
func prepareExcludeSpec(root string, exclude []string, includeHidden, useGitignore bool) (*ignore.GitIgnore, error) {
	var lines []string
	if !includeHidden {
		lines = append(lines, DefaultGlobs...)
	}
	if useGitignore {
		fi, err := os.Stat(root)
		if err == nil && fi.IsDir() {
			gi, err := getGitignorePatterns(root)
			if err != nil {
				return nil, err
			}
			lines = append(lines, gi...)
		}
	}
	lines = append(lines, exclude...)
	return ignore.CompileIgnoreLines(dedupeFirst(lines)...), nil
}

func getGitignorePatterns(root string) ([]string, error) {
	p := filepath.Join(root, ".gitignore")
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return nil, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return splitlines(data), nil
}

func (e *Extractor) isExcluded(path string, isDir bool) bool {
	rel, err := filepath.Rel(e.root, path)
	if err != nil {
		return e.isOutPath(path)
	}
	rel = filepath.ToSlash(rel)
	// Never exclude scan root (DefaultGlobs ".*" matches ".").
	if rel == "." || strings.HasPrefix(rel, "../") {
		return false
	}
	if e.isOutPath(path) {
		return true
	}
	return excludeMatch(e.gi, rel, isDir)
}

func (e *Extractor) isOutPath(path string) bool {
	if e.outAbs == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	return err == nil && absPath == e.outAbs
}

func splitlines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	s := string(data)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	parts := strings.Split(s, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}
