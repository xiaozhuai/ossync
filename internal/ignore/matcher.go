package ignore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// Matcher applies gitignore-compatible patterns and a set of paths that must
// never be uploaded (notably the credentials file).
type Matcher struct {
	patterns  *gitignore.GitIgnore
	protected map[string]struct{}
}

func Load(filename string, required bool, protected []string) (*Matcher, error) {
	m := &Matcher{protected: make(map[string]struct{}, len(protected))}
	for _, item := range protected {
		item = normalize(item)
		if item != "" && item != "." {
			m.protected[item] = struct{}{}
		}
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return m, nil
		}
		return nil, fmt.Errorf("read ignore file %q: %w", filename, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	m.patterns = gitignore.CompileIgnoreLines(lines...)
	return m, nil
}

func (m *Matcher) Match(relativePath string) bool {
	relativePath = normalize(relativePath)
	if _, ok := m.protected[relativePath]; ok {
		return true
	}
	return m.patterns != nil && m.patterns.MatchesPath(relativePath)
}

func normalize(value string) string {
	return filepath.ToSlash(strings.TrimPrefix(filepath.Clean(value), "./"))
}
