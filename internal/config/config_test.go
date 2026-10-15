package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTarget(t *testing.T) {
	tests := []struct {
		in     string
		bucket string
		prefix string
	}{
		{"oss://bucket", "bucket", ""},
		{"oss://bucket/", "bucket", ""},
		{"oss://bucket/path/to/site", "bucket", "path/to/site/"},
		{"oss://bucket//path/to/site/", "bucket", "path/to/site/"},
	}
	for _, tt := range tests {
		target, err := ParseTarget(tt.in)
		if err != nil {
			t.Fatalf("ParseTarget(%q): %v", tt.in, err)
		}
		if target.Bucket != tt.bucket || target.Prefix != tt.prefix {
			t.Errorf("ParseTarget(%q) = %#v", tt.in, target)
		}
	}
}

func TestParseTargetRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "bucket/path", "oss:///path", "oss://bucket/path?x=1"} {
		if _, err := ParseTarget(value); err == nil {
			t.Errorf("ParseTarget(%q) unexpectedly succeeded", value)
		}
	}
}

func TestLoadRejectsOSSRegionPrefix(t *testing.T) {
	filename := filepath.Join(t.TempDir(), ".ossyncconfig")
	contents := "access_key_id: id\n" +
		"access_key_secret: secret\n" +
		"prefix: oss://bucket/path\n" +
		"region: oss-cn-shanghai\n"
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := Load(filename)
	if err == nil || !strings.Contains(err.Error(), "region must not use the oss- prefix") {
		t.Fatalf("Load() error = %v", err)
	}
}
