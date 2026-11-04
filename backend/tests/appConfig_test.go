package tests

import (
	"fmt"
	"testing"

	"houridle/backend/config"
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
