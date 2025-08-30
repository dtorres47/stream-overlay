package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/dtorres47/stream-overlay/internal/catalog"
	"github.com/dtorres47/stream-overlay/internal/quests"
	"github.com/dtorres47/stream-overlay/internal/requests"
	"github.com/dtorres47/stream-overlay/internal/state"
	"github.com/dtorres47/stream-overlay/internal/tts"
	"github.com/dtorres47/stream-overlay/internal/ws"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed web/index.html
var overlayHTML []byte

//go:embed web/panel.html
var panelHTML []byte

func main() {
	// Load catalog (abilities/quests) and restore any saved state
	catalog.LoadCatalogFromDisk()
	state.LoadState()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.RedirectSlashes)

	// Index page
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<h3>Stream Overlay</h3>
<p>Connected overlays: %d</p>
<ul>
  <li><a href="/overlay" target="_blank">/overlay</a></li>
  <li><a href="/panel" target="_blank">/panel</a></li>
  <li><a href="/api/debug/clients" target="_blank">/api/debug/clients</a></li>
</ul>`, ws.ClientsCount())
	})

	// Overlay + WebSocket endpoint
	r.Get("/overlay", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(overlayHTML)
	})
	r.Get("/ws", ws.WSHandler)

	// Control panel
	r.Get("/panel", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(panelHTML)
	})

	// API: catalog endpoints (list & reload)
	catalog.RegisterRoutes(r)

	// API: quest endpoints (add, list active, inc, reset, remove)
	quests.RegisterRoutes(r)

	// API: text-to-speech endpoints (submit, queue, approve, reject)
	tts.RegisterRoutes(r)

	// API: request endpoints (submit, queue, active, approve, reject, complete)
	requests.RegisterRoutes(r)

	// API: persistence helpers (save & rehydrate state.json)
	state.RegisterRoutes(r)

	// Debug: number of WS clients
	r.Get("/api/debug/clients", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%d\n", ws.ClientsCount())
	})

	// Health check for CI/CD & AWS
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok","service":"stream-overlay"}`)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	addr := ":" + port

	log.Printf("Server listening on http://localhost%v", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
