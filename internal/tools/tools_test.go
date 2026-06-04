package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/browsersdk/brosdk-mcp-go/internal/recorder"
)

func TestScenePathRejectsTraversal(t *testing.T) {
	oldDir := ScenesDir()
	SetScenesDir(t.TempDir())
	defer SetScenesDir(oldDir)

	badNames := []string{"../escape", `..\escape`, "/abs", `C:\abs`, "", ".hidden"}
	for _, name := range badNames {
		if _, err := scenePath(name); err == nil {
			t.Fatalf("scenePath(%q) expected error", name)
		}
	}

	path, err := scenePath("login_flow-1.2")
	if err != nil {
		t.Fatalf("scenePath valid name: %v", err)
	}
	if !strings.HasPrefix(path, ScenesDir()) {
		t.Fatalf("scene path %q should stay under %q", path, ScenesDir())
	}
}

func TestRecordStopOverwritesExistingScene(t *testing.T) {
	oldDir := ScenesDir()
	dir := t.TempDir()
	SetScenesDir(dir)
	defer SetScenesDir(oldDir)

	path := filepath.Join(dir, "existing.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"name":"existing","steps":[]}`), 0644); err != nil {
		t.Fatalf("write existing scene: %v", err)
	}

	rec := recorder.Get()
	_, _ = rec.Stop()
	if err := rec.Start(); err != nil {
		t.Fatalf("record start: %v", err)
	}
	rec.Capture("browser_navigate", map[string]any{"url": "https://example.com"})
	if _, err := Dispatch(nil, "record_stop", map[string]any{"name": "existing"}); err != nil {
		t.Fatalf("record_stop should overwrite existing scene: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read overwritten scene: %v", err)
	}
	if strings.Contains(string(data), `"steps":[]`) {
		t.Fatalf("record_stop did not overwrite old scene content: %s", data)
	}
}
