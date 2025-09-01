package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/dtorres47/stream-overlay/internal/catalog"
	"github.com/dtorres47/stream-overlay/internal/history"
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
	// Load catalog & restore saved state
	//catalog.LoadCatalogFromDisk()
	catalog.LoadCatalog()
	state.LoadState()

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.RedirectSlashes)

	// Serve static assets (css/js)
	r.Handle("/css/*", http.StripPrefix("/css/", http.FileServer(http.Dir("web/css"))))
	r.Handle("/js/*", http.StripPrefix("/js/", http.FileServer(http.Dir("web/js"))))
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir("web/assets"))))

	// Serve config & data JSON
	r.Handle("/config/*", http.StripPrefix("/config/", http.FileServer(http.Dir("web/config"))))
	r.Handle("/data/*", http.StripPrefix("/data/", http.FileServer(http.Dir("web/data"))))

	// And for your overlay & panel-relative paths:
	r.Handle("/overlay/css/*", http.StripPrefix("/overlay/css/", http.FileServer(http.Dir("web/css"))))
	r.Handle("/overlay/js/*", http.StripPrefix("/overlay/js/", http.FileServer(http.Dir("web/js"))))

	r.Handle("/panel/css/*", http.StripPrefix("/panel/css/", http.FileServer(http.Dir("web/css"))))
	r.Handle("/panel/js/*", http.StripPrefix("/panel/js/", http.FileServer(http.Dir("web/js"))))

	// Home page
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<h3>Stream Overlay</h3>
<p>Connected overlays: %d</p>
<ul>
  <li><a href="/overlay" target="_blank">Overlay</a></li>
  <li><a href="/panel" target="_blank">Panel</a></li>
  <li><a href="/api/debug/clients" target="_blank">Client Count</a></li>
</ul>`, ws.ClientsCount())
	})

	// Overlay + WS
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

	// API routes
	catalog.RegisterRoutes(r)
	quests.RegisterRoutes(r)
	tts.RegisterRoutes(r)
	requests.RegisterRoutes(r)
	state.RegisterRoutes(r)

	// Donation-history endpoint
	r.Post("/api/donations", history.RecordDonation)

	// Debug & health
	r.Get("/api/debug/clients", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%d\n", ws.ClientsCount())
	})
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
