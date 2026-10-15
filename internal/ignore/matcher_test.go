package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMatcher(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, ".ossyncignore")
	contents := "*.log\nbuild/\n!important.log\n"
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	matcher, err := Load(filename, true, []string{"secret.yml"})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]bool{
		"debug.log":       true,
		"logs/debug.log":  true,
		"important.log":   false,
		"build/output.js": true,
		"src/main.go":     false,
		"secret.yml":      true,
	}
	for path, want := range tests {
		if got := matcher.Match(path); got != want {
			t.Errorf("Match(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestOptionalMissingIgnoreFile(t *testing.T) {
	matcher, err := Load(filepath.Join(t.TempDir(), "missing"), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if matcher.Match("anything") {
		t.Fatal("empty matcher ignored a path")
	}
}
