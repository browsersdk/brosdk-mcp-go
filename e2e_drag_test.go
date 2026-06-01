//go:build windows

package main_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func startDragTestServer(t *testing.T) (string, func()) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Drag & Upload Test</title>
<style>
  #source { width:100px;height:100px;background:#90caf9;display:flex;align-items:center;justify-content:center;cursor:grab; }
  #target { width:200px;height:200px;border:2px dashed #ccc;margin-top:20px;display:flex;align-items:center;justify-content:center; }
  #target.hover { border-color: #4caf50; background: #e8f5e9; }
  #uploadResult { margin-top:10px; }
</style>
</head><body>
<div id="app">
  <h1>Drag & Upload Test</h1>
  <div id="source" draggable="true">Drag Me</div>
  <div id="target">Drop Here</div>
  <div id="dragLog"></div>
  <div id="output"></div>
  <hr>
  <input type="file" id="fileInput">
  <div id="uploadResult"></div>
</div>
<script>
(function(){
  var o=document.getElementById('output'),dl=document.getElementById('dragLog'),
      src=document.getElementById('source'),tgt=document.getElementById('target'),
      fi=document.getElementById('fileInput'),ur=document.getElementById('uploadResult');
  src.addEventListener('dragstart',function(e){e.dataTransfer.setData('text/plain','dragged');dl.textContent='dragstart;';});
  src.addEventListener('dragend',function(){dl.textContent+='dragend;';});
  tgt.addEventListener('dragenter',function(e){e.preventDefault();tgt.classList.add('hover');});
  tgt.addEventListener('dragleave',function(){tgt.classList.remove('hover');});
  tgt.addEventListener('dragover',function(e){e.preventDefault();});
  tgt.addEventListener('drop',function(e){
    e.preventDefault();
    tgt.classList.remove('hover');
    o.textContent='dropped:'+e.dataTransfer.getData('text/plain');
  });
  fi.addEventListener('change',function(){ur.textContent=fi.files.length+' file(s) selected';});
})();
</script>
</body></html>`)
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(listener)

	addr := fmt.Sprintf("http://%s", listener.Addr().String())
	return addr, func() { srv.Close() }
}


// ======================================================================
// TestE2E_DragAndUpload — covers drag, upload_file
// ======================================================================

func TestE2E_DragAndUpload(t *testing.T) {
	f := setupE2E(t)
	defer f.cleanup()

	testURL, stopServer := startDragTestServer(t)
	defer stopServer()
	t.Logf("drag server: %s", testURL)

	envID := f.getFirstEnvID()
	sessionID := browserOpenNavigate(t, f, envID, testURL)
	defer browserCloseHelper(t, f, envID)

	// ── 1. browser_drag ──
	t.Run("drag", func(t *testing.T) {
		_, err := f.client.callTool("browser_drag", map[string]any{
			"envId":          envID,
			"sourceSelector": "#source",
			"targetSelector": "#target",
			"sessionId":      sessionID,
		})
		if err != nil {
			t.Fatalf("drag: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		out := evalText(t, f, envID, `document.getElementById('output').textContent`)
		if !strings.Contains(out, "dropped") {
			t.Errorf("drag: expected dropped in output, got %q", out)
		}
		t.Logf("drag ✓ output=%s", out)
	})

	// ── 2. browser_upload_file ──
	t.Run("upload_file", func(t *testing.T) {
		// Create a temporary file to upload
		tmpFile := filepath.Join(os.TempDir(), "brosdk-e2e-upload.txt")
		os.WriteFile(tmpFile, []byte("test content"), 0644)
		defer os.Remove(tmpFile)

		_, err := f.client.callTool("browser_upload_file", map[string]any{
			"envId":     envID,
			"selector":  "#fileInput",
			"files":     []any{tmpFile},
			"sessionId": sessionID,
		})
		if err != nil {
			t.Fatalf("upload_file: %v", err)
		}
		time.Sleep(300 * time.Millisecond)
		result := evalText(t, f, envID, `document.getElementById('uploadResult').textContent`)
		if !strings.Contains(result, "1 file") {
			t.Errorf("upload_file: expected '1 file(s) selected', got %q", result)
		}
		t.Logf("upload_file ✓ result=%s", result)
	})

	t.Log("drag & upload test complete ✓")
}
