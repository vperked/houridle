package tests

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"houridle/backend/run/config"
)

func TestParseAppIDs(t *testing.T) {
	ids, err := config.ParseAppIDs([]string{"730, 440", "570,730", " 10 "})
	if err != nil || fmt.Sprint(config.Dedupe(ids)) != "[730 440 570 10]" {
		t.Fatalf("got %v, %v", ids, err)
	}
	for _, bad := range []string{"abc", "0", "-5", "99999999999"} {
		if _, err := config.ParseAppIDs([]string{bad}); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestConfigSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	if cfg, err := config.Load(dir); err != nil || len(cfg.Games) != 0 {
		t.Fatalf("a missing config should load empty, got %v, %v", cfg, err)
	}
	if err := config.Save(dir, config.Config{Games: []uint32{730, 440}}); err != nil {
		t.Fatal(err)
	}
	if cfg, err := config.Load(dir); err != nil || fmt.Sprint(cfg.Games) != "[730 440]" {
		t.Fatalf("round trip gave %v, %v", cfg, err)
	}
}

func TestConfigSaveReplacesCorruptFileAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"games": [730,`), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, err := config.Load(dir); err == nil || len(cfg.Games) != 0 {
		t.Fatalf("a half-written config should be reported and ignored, got %v, %v", cfg, err)
	}
	if err := config.Save(dir, config.Config{Games: []uint32{440}}); err != nil {
		t.Fatal(err)
	}
	if cfg, err := config.Load(dir); err != nil || fmt.Sprint(cfg.Games) != "[440]" {
		t.Fatalf("after saving, got %v, %v", cfg, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("saving should leave only config.json behind, found %d files", len(entries))
	}
}

func TestAppDirFallsBackWhenExeFolderIsntWritable(t *testing.T) {
	writableDir := t.TempDir()
	if got := config.AppDirFor(filepath.Join(writableDir, "houridle.exe"), filepath.Join(t.TempDir(), "houridle")); got != writableDir {
		t.Fatalf("a writable exe folder should be used, got %q", got)
	}

	missing := filepath.Join(t.TempDir(), "no-such-folder")
	fallback := filepath.Join(t.TempDir(), "houridle")
	if got := config.AppDirFor(filepath.Join(missing, "houridle.exe"), fallback); got != fallback {
		t.Fatalf("an unwritable exe folder should fall back to %q, got %q", fallback, got)
	}
	if info, err := os.Stat(fallback); err != nil || !info.IsDir() {
		t.Fatalf("the fallback folder should be created: %v", err)
	}
}

func TestAppDirDetectsGoRunOnlyInItsTempFolder(t *testing.T) {
	goTmp := t.TempDir()
	t.Setenv("GOTMPDIR", goTmp)
	wd, _ := os.Getwd()

	goRun := filepath.Join(goTmp, "go-build123", "b001", "exe")
	if err := os.MkdirAll(goRun, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := config.AppDirFor(filepath.Join(goRun, "houridle.exe"), ""); got != wd {
		t.Fatalf("under go run the working directory should be used, got %q", got)
	}

	installed := filepath.Join(t.TempDir(), "go-builds", "houridle")
	if err := os.MkdirAll(installed, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := config.AppDirFor(filepath.Join(installed, "houridle.exe"), ""); got != installed {
		t.Fatalf("a go-build lookalike folder should be used, got %q", got)
	}
}

func TestFlagsFirst(t *testing.T) {
	fs := flag.NewFlagSet("houridle", flag.ContinueOnError)
	fs.Bool("setup", false, "")
	fs.Bool("tray", false, "")
	fs.String("stop", "", "")
	fs.String("dll", "", "")
	cases := map[string]string{
		"730 440 -stop 3h":                "-stop 3h -- 730 440",
		"start 730 -setup -dll /x/lib.so": "-setup -dll /x/lib.so -- start 730",
		"-setup start":                    "-setup -- start",
		"-stop=90m 730":                   "-stop=90m -- 730",
		"-tray -stop never":               "-tray -stop never",
		"730 -- -5":                       "-- 730 -5",
	}
	for in, want := range cases {
		if got := strings.Join(config.FlagsFirst(fs, strings.Fields(in)), " "); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}

	stop := fs.Lookup("stop")
	if err := fs.Parse(config.FlagsFirst(fs, []string{"start", "730", "-stop", "3h", "440"})); err != nil {
		t.Fatal(err)
	}
	if stop.Value.String() != "3h" || strings.Join(fs.Args(), " ") != "start 730 440" {
		t.Fatalf("parsed -stop=%q args=%v", stop.Value.String(), fs.Args())
	}
}
