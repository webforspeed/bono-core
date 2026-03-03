package codesearch

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Always-ignored directories and extensions.
var (
	alwaysIgnoreDirs = map[string]bool{
		".git":         true,
		".bono":        true,
		".hg":          true,
		".svn":         true,
		"node_modules": true,
		"__pycache__":  true,
		".tox":         true,
		".mypy_cache":  true,
		".pytest_cache": true,
	}

	binaryExtensions = map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true, ".a": true,
		".o": true, ".obj": true, ".lib": true,
		".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true, ".7z": true, ".rar": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true, ".ico": true, ".webp": true, ".svg": true,
		".mp3": true, ".mp4": true, ".avi": true, ".mov": true, ".flac": true, ".wav": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
		".wasm": true, ".pyc": true, ".pyo": true, ".class": true,
		".db": true, ".sqlite": true, ".sqlite3": true,
		".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
	}
)

// GitIgnore filters file paths based on .gitignore rules and built-in exclusions.
type GitIgnore struct {
	rootDir  string
	patterns []ignorePattern
}

type ignorePattern struct {
	pattern  string
	negate   bool
	dirOnly  bool
	baseName bool // true if pattern has no slash → match against base name only
}

// LoadGitIgnore reads .gitignore from rootDir and returns a matcher.
// Returns a valid (empty) GitIgnore even if no .gitignore exists.
func LoadGitIgnore(rootDir string) *GitIgnore {
	gi := &GitIgnore{rootDir: rootDir}

	// Load root .gitignore
	gi.loadFile(filepath.Join(rootDir, ".gitignore"))

	return gi
}

func (gi *GitIgnore) loadFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := ignorePattern{}

		// Negation
		if strings.HasPrefix(line, "!") {
			p.negate = true
			line = line[1:]
		}

		// Directory-only pattern
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}

		// If pattern has no slash, match against base name only
		if !strings.Contains(line, "/") {
			p.baseName = true
		} else {
			// Remove leading slash for matching
			line = strings.TrimPrefix(line, "/")
		}

		p.pattern = line
		gi.patterns = append(gi.patterns, p)
	}
}

// ShouldIgnore returns true if the given path (relative to rootDir) should be excluded.
func (gi *GitIgnore) ShouldIgnore(relPath string) bool {
	// Always ignore certain directories
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for _, part := range parts {
		if alwaysIgnoreDirs[part] {
			return true
		}
	}

	// Always ignore binary files
	ext := strings.ToLower(filepath.Ext(relPath))
	if binaryExtensions[ext] {
		return true
	}

	// Apply .gitignore patterns (last matching pattern wins)
	ignored := false
	for _, p := range gi.patterns {
		if gi.matchPattern(p, relPath) {
			ignored = !p.negate
		}
	}

	return ignored
}

// ShouldIgnoreDir returns true if a directory should be skipped entirely.
func (gi *GitIgnore) ShouldIgnoreDir(relPath string) bool {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for _, part := range parts {
		if alwaysIgnoreDirs[part] {
			return true
		}
	}

	// Check gitignore patterns
	ignored := false
	for _, p := range gi.patterns {
		if gi.matchPattern(p, relPath) || gi.matchPattern(p, relPath+"/") {
			ignored = !p.negate
		}
	}
	return ignored
}

func (gi *GitIgnore) matchPattern(p ignorePattern, relPath string) bool {
	relPath = filepath.ToSlash(relPath)

	var toMatch string
	if p.baseName {
		toMatch = filepath.Base(relPath)
	} else {
		toMatch = relPath
	}

	matched, _ := filepath.Match(p.pattern, toMatch)
	if matched {
		return true
	}

	// Try matching with ** prefix for deeper paths
	if !p.baseName && strings.Contains(p.pattern, "*") {
		// Also try each suffix of the path
		parts := strings.Split(relPath, "/")
		for i := range parts {
			sub := strings.Join(parts[i:], "/")
			if m, _ := filepath.Match(p.pattern, sub); m {
				return true
			}
		}
	}

	return false
}
