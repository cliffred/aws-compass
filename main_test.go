package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
)

// parse runs the kong parser the same way main does, without exiting.
func parse(t *testing.T, args ...string) (*CLI, error) {
	t.Helper()
	var cli CLI
	parser, err := kong.New(&cli, kong.Vars{"version": "test"}, kong.Exit(func(int) {}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = parser.Parse(args)
	return &cli, err
}

func TestParseFlagsAfterConnection(t *testing.T) {
	cli, err := parse(t, "conn", "--print", "-p", "prof")
	if err != nil {
		t.Fatal(err)
	}
	if cli.Connection != "conn" || !cli.Print || cli.Profile != "prof" {
		t.Errorf("got %+v", cli)
	}
}

func TestParseRejectsInvalidCombinations(t *testing.T) {
	for _, args := range [][]string{
		{"conn", "--reveal"},
		{"conn", "--mongosh", "--print"},
		{"conn", "--mongosh", "--copy"},
		{"--save", "x"},
		{"--save", "x", "--uri", "mongodb://h", "conn"},
		{"--save", "x", "--profile", "p", "--print"},
	} {
		if _, err := parse(t, args...); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}

func TestSaveCreatesAndUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	var stderr bytes.Buffer

	add := &CLI{Save: "c1", URI: "mongodb+srv://h1", Profile: "p1"}
	if err := add.save(&stderr, path); err != nil {
		t.Fatal(err)
	}
	update := &CLI{Save: "c1", Profile: "p2"}
	if err := update.save(&stderr, path); err != nil {
		t.Fatal(err)
	}

	cfg, found, err := loadConfig(path, true)
	if err != nil || !found {
		t.Fatalf("loadConfig: found=%v err=%v", found, err)
	}
	if got := cfg.Connections["c1"]; got.URI != "mongodb+srv://h1" || got.Profile != "p2" {
		t.Errorf("c1 = %+v", got)
	}
	if !strings.Contains(stderr.String(), `Added "c1"`) || !strings.Contains(stderr.String(), `Updated "c1"`) {
		t.Errorf("stderr = %q", stderr.String())
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "defaults") {
		t.Errorf("empty defaults written:\n%s", data)
	}
}

func TestSaveNewConnectionNeedsURI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cli := &CLI{Save: "c1", Profile: "p1"}
	if err := cli.save(&bytes.Buffer{}, path); err == nil {
		t.Error("expected an error")
	}
	if fileExists(path) {
		t.Error("config file created despite the error")
	}
}

func TestSaveRejectsURIWithCredentials(t *testing.T) {
	cli := &CLI{Save: "c1", URI: "mongodb://user:pw@h"}
	if err := cli.save(&bytes.Buffer{}, filepath.Join(t.TempDir(), "config.json")); err == nil {
		t.Error("expected an error")
	}
}

func TestSaveFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "dotfiles.json")
	link := filepath.Join(dir, "config.json")
	if err := os.WriteFile(real, []byte(`{"connections":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	cli := &CLI{Save: "c1", URI: "mongodb://h"}
	if err := cli.save(&bytes.Buffer{}, link); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink == 0 {
		t.Error("symlink was replaced by a regular file")
	}
	if data, _ := os.ReadFile(real); !strings.Contains(string(data), "c1") {
		t.Errorf("target not updated:\n%s", data)
	}
}
