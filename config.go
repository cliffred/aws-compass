package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config is the contents of the aws-compass config file.
type Config struct {
	Defaults    Defaults              `json:"defaults,omitzero"`
	Connections map[string]Connection `json:"connections,omitempty"`
}

// Defaults apply to every connection unless the connection overrides them.
type Defaults struct {
	// Profile is the AWS profile used when a connection has none.
	Profile string `json:"profile,omitempty"`
	// CompassPath is the MongoDB Compass executable, e.g. for Compass Beta
	// or Isolated Edition.
	CompassPath string `json:"compassPath,omitempty"`
	// MongoshPath is the mongosh executable.
	MongoshPath string `json:"mongoshPath,omitempty"`
}

// Connection is one named cluster.
type Connection struct {
	// URI is the connection string without credentials. It may contain a
	// database and query parameters such as appName or readPreference.
	URI string `json:"uri"`
	// Profile is the AWS profile (from ~/.aws/config) to get credentials from.
	Profile string `json:"profile,omitempty"`
}

// configPath returns the config file to use and whether the user chose it
// explicitly. Order: --config, $AWS_COMPASS_CONFIG,
// $XDG_CONFIG_HOME/aws-compass/config.json (~/.config if unset),
// then the legacy ~/.aws-compass/config.
func configPath(flagValue string) (string, bool, error) {
	if flagValue != "" {
		p, err := expandHome(flagValue)
		return p, true, err
	}
	if env := os.Getenv("AWS_COMPASS_CONFIG"); env != "" {
		p, err := expandHome(env)
		return p, true, err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, fmt.Errorf("finding home directory: %w", err)
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	primary := filepath.Join(base, "aws-compass", "config.json")
	if fileExists(primary) {
		return primary, false, nil
	}
	for _, legacy := range []string{
		filepath.Join(home, ".aws-compass", "config"),
		filepath.Join(home, ".aws-compass", "config.json"),
	} {
		if fileExists(legacy) {
			return legacy, false, nil
		}
	}
	return primary, false, nil
}

// loadConfig reads the config file. A missing file is only an error when the
// user pointed at it explicitly; otherwise an empty config is returned and
// found is false.
func loadConfig(path string, explicit bool) (cfg *Config, found bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !explicit {
			return &Config{}, false, nil
		}
		return nil, false, fmt.Errorf("reading config: %w", err)
	}

	cfg = &Config{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // catch typos like "profiel"
	if err := dec.Decode(cfg); err != nil {
		return nil, true, fmt.Errorf("parsing %s: %w", path, err)
	}
	for name, c := range cfg.Connections {
		if strings.TrimSpace(name) == "" {
			return nil, true, fmt.Errorf("%s: connection with an empty name", path)
		}
		if strings.TrimSpace(c.URI) == "" {
			return nil, true, fmt.Errorf("%s: connection %q has no uri", path, name)
		}
	}
	return cfg, true, nil
}

// saveConfig writes cfg as indented JSON. It writes a temporary file and
// renames it, so a failed write never leaves a half-written config, and it
// follows a symlinked config (e.g. into a dotfiles repo) instead of replacing
// the link.
func saveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// names returns the connection names in sorted order.
func (c *Config) names() []string {
	names := make([]string, 0, len(c.Connections))
	for n := range c.Connections {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("finding home directory: %w", err)
		}
		return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
	}
	return p, nil
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
