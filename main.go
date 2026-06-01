// brosdk-mcp-go – BroSDK MCP SSE server
//
// Usage:
//
//	brosdk-mcp-go -lib ./libs/windows-x64/brosdk.dll -addr :8765      (Windows)
//	brosdk-mcp-go -lib ./libs/darwin-arm64/libbrosdk.dylib -addr :8765  (macOS)
//	brosdk-mcp-go -addr :8765                                          (auto-download on first run)
//
// The server exposes a single SSE endpoint (GET /sse) and a message
// endpoint (POST /message) as defined by the MCP 2024-11-05 spec.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"
	"syscall"

	"github.com/browsersdk/brosdk-mcp-go/internal/brosdk"
	"github.com/browsersdk/brosdk-mcp-go/internal/config"
	"github.com/browsersdk/brosdk-mcp-go/internal/mcp"
	"github.com/browsersdk/brosdk-mcp-go/internal/tools"
)

const version = "0.1.0"

func main() {
	libFlag := flag.String("lib", "", "Path to brosdk native library (auto-detect if empty)")
	addr := flag.String("addr", ":8765", "SSE server listen address")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[brosdk-mcp] ")

	// ── Load native SDK library (auto-download if missing) ────────────────────
	libPath, err := brosdk.EnsureLibrary(*libFlag)
	if err != nil {
		log.Fatalf("library setup failed: %v", err)
	}
	mgr := brosdk.NewManager()
	if err := mgr.Load(libPath); err != nil {
		log.Fatalf("failed to load brosdk library %q: %v", libPath, err)
	}
	log.Printf("BroSDK library loaded from %s", libPath)

	// ── Auto-init SDK from config ────────────────────────────────────────────
	cfg, err := loadConfigOrNil()
	if err != nil {
		log.Fatalf("failed to read config: %v", err)
	}
	if cfg != nil {
		opts := brosdk.InitOptions{
			UserSig:   cfg.UserSig,
			ApiKey:    cfg.ApiKey,
			WorkDir:   cfg.WorkDir,
			Port:      cfg.Port,
			SdkApiURL: cfg.SdkApiURL,
			Debug:     cfg.Debug,
		}
		if opts.WorkDir == "" {
			opts.WorkDir = filepath.Join(".", "brosdk")
		}
		if opts.Port <= 0 {
			opts.Port = 5811
		}
		if err := os.MkdirAll(opts.WorkDir, 0755); err != nil {
			log.Fatalf("failed to create workDir %q: %v", opts.WorkDir, err)
		}
		log.Printf("auto-init: apiKey=%s port=%d workDir=%s", maskString(opts.ApiKey), opts.Port, opts.WorkDir)
		resp, err := mgr.Init(opts)
		if err != nil {
			log.Fatalf("sdk_init failed: %v", err)
		}
		log.Printf("sdk_init ok, code=%d response=%s", resp.Code, resp.Response)
	} else {
		log.Println("no config file found, skipping auto-init (provide config.json with apiKey)")
	}

	// ── Wire SDK events → SSE broadcast ─────────────────────────────────────
	srv := mcp.NewServer(
		mcp.ServerInfo{Name: "brosdk-mcp", Version: version},
		tools.All(),
		tools.Handler(mgr),
	)

	mgr.OnEvent(func(evt brosdk.Event) {
		data, _ := json.Marshal(evt)
		srv.Broadcast("sdk-event", string(data))
		log.Printf("sdk-event code=%d data=%s", evt.Code, evt.Data)
	})

	// ── HTTP mux ─────────────────────────────────────────────────────────────
	mux := http.NewServeMux()
	srv.Register(mux)

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	httpSrv := &http.Server{Addr: *addr, Handler: mux}

	go func() {
		<-stop
		log.Println("shutting down...")
		if mgr.Loaded() {
			if err := mgr.Shutdown(); err != nil {
				log.Printf("sdk shutdown error: %v", err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Printf("http server shutdown error: %v", err)
		}
	}()

	log.Printf("MCP SSE server listening on %s", *addr)
	log.Printf("  Inspector    : http://localhost%s/inspector", *addr)
	log.Printf("  SSE endpoint : http://localhost%s/sse", *addr)
	log.Printf("  POST endpoint: http://localhost%s/message", *addr)
	log.Printf("  Health check : http://localhost%s/health", *addr)

	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http server error: %v", err)
	}
}

// resolveLibPath determines the brosdk library path for the current platform.
// Order: explicit -lib flag → local candidates → auto-download from GitHub.
// This is the legacy fallback; EnsureLibrary in download.go does the full logic.
func resolveLibPath(explicit string) string {
	return explicit
}

// loadConfigOrNil reads config.local.json → config.json in order.
// Returns (nil, nil) when neither file is present.
func loadConfigOrNil() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// maskString returns a shortened, masked version for logging.
func maskString(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "***" + s[len(s)-4:]
}
