package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the on-disk .ossyncconfig format.
type Config struct {
	AccessKeyID     string `yaml:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret"`
	Prefix          string `yaml:"prefix"`
	Region          string `yaml:"region"`
}

// Target identifies the bucket and the key prefix within it. Prefix is empty or
// ends in a slash, making joins with relative local paths unambiguous.
type Target struct {
	Bucket string
	Prefix string
}

func Load(filename string) (Config, Target, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, Target{}, fmt.Errorf("read config %q: %w", filename, err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, Target{}, fmt.Errorf("parse config %q: %w", filename, err)
	}

	cfg.AccessKeyID = strings.TrimSpace(cfg.AccessKeyID)
	cfg.AccessKeySecret = strings.TrimSpace(cfg.AccessKeySecret)
	cfg.Prefix = strings.TrimSpace(cfg.Prefix)
	cfg.Region = strings.TrimSpace(cfg.Region)
	if cfg.AccessKeyID == "" {
		return Config{}, Target{}, fmt.Errorf("config %q: access_key_id is required", filename)
	}
	if cfg.AccessKeySecret == "" {
		return Config{}, Target{}, fmt.Errorf("config %q: access_key_secret is required", filename)
	}
	if cfg.Region == "" {
		return Config{}, Target{}, fmt.Errorf("config %q: region is required", filename)
	}
	if strings.HasPrefix(cfg.Region, "oss-") {
		return Config{}, Target{}, fmt.Errorf("config %q: region must not use the oss- prefix", filename)
	}

	target, err := ParseTarget(cfg.Prefix)
	if err != nil {
		return Config{}, Target{}, fmt.Errorf("config %q: %w", filename, err)
	}
	return cfg, target, nil
}

func ParseTarget(value string) (Target, error) {
	if !strings.HasPrefix(value, "oss://") {
		return Target{}, fmt.Errorf("prefix must use oss://bucket/path format")
	}
	remainder := strings.TrimPrefix(value, "oss://")
	bucket, prefix, found := strings.Cut(remainder, "/")
	if !found {
		prefix = ""
	}
	if bucket == "" {
		return Target{}, fmt.Errorf("prefix bucket is empty")
	}
	if strings.ContainsAny(bucket, "\\?#") {
		return Target{}, fmt.Errorf("prefix contains an invalid bucket name")
	}
	if strings.ContainsAny(prefix, "\\?#") {
		return Target{}, fmt.Errorf("prefix path must not contain \\, ? or #")
	}
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return Target{Bucket: bucket, Prefix: prefix}, nil
}
