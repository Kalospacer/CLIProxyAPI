package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestConfigReloadDoesNotMarkLaterWriteAsApplied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	first := []byte(fmt.Sprintf("port: 8317\nauth-dir: %q\nrequest-retry: 1\n", filepath.ToSlash(dir)))
	second := []byte(fmt.Sprintf("port: 8317\nauth-dir: %q\nrequest-retry: 2\n", filepath.ToSlash(dir)))
	if err := os.WriteFile(path, first, 0600); err != nil {
		t.Fatal(err)
	}
	var observed []int
	w := &Watcher{configPath: path, authDir: dir, config: &config.Config{AuthDir: dir}}
	w.reloadCallback = func(cfg *config.Config) {
		observed = append(observed, cfg.RequestRetry)
		if len(observed) == 1 {
			if err := os.WriteFile(path, second, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	w.ReloadConfigIfChanged()
	w.ReloadConfigIfChanged()
	if len(observed) != 2 || observed[0] != 1 || observed[1] != 2 {
		t.Fatalf("later disk write was skipped: %v", observed)
	}
}
