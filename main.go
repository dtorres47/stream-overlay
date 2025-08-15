package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
)

// ─── Embed your existing overlay.html (keep that file as-is) ────────────────
//
//go:embed overlay.html
var overlayHTML []byte

//go:embed panel.html
var panelHTML []byte

// ─── WebSocket plumbing ─────────────────────────────────────────────────────
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true }, // allow OBS/browser
}

var (
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
)

type wsMsg struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

func broadcast(m wsMsg) int {
	b, _ := json.Marshal(m)
	clientsMu.Lock()
	defer clientsMu.Unlock()
	n := 0
	for c := range clients {
		if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
			log.Println("WS write error:", err)
			_ = c.Close()
			delete(clients, c)
		} else {
			n++
		}
	}
	log.Printf("broadcast %q to %d client(s)", m.Type, n)
	return n
}

func clientsCount() int {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	return len(clients)
}

// ─── tiny in-memory catalog (for testing abilities/quests) ──────────────────
type Ability struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	SFXURL     string `json:"sfx_url"`
	IconURL    string `json:"icon_url"`
}
type Quest struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	IconURL    string `json:"icon_url"`
}

var (
	abilities = map[string]Ability{}
	quests    = map[string]Quest{}
)

type catalogFile struct {
	Abilities []Ability `json:"abilities"`
	Quests    []Quest   `json:"quests"`
}

func loadCatalogFromDisk() {
	b, err := os.ReadFile("catalog.json")
	if err != nil {
		log.Println("catalog.json not found; using built-in defaults")
		// Minimal defaults if file missing
		abilities = map[string]Ability{
			"trex": {ID: "trex", Name: "T-Rex Roar", PriceCents: 300, SFXURL: "https://interactive-examples.mdn.mozilla.net/media/cc0-audio/t-rex-roar.mp3"},
		}
		quests = map[string]Quest{
			"call-maam":     {ID: "call-maam", Name: `Call a man "ma'am"`, PriceCents: 499},
			"soundboard-5x": {ID: "soundboard-5x", Name: "Make 5 calls with X soundboard", PriceCents: 399},
		}
		return
	}
	var cf catalogFile
	if err := json.Unmarshal(b, &cf); err != nil {
		log.Println("catalog.json parse error:", err)
		return
	}
	abilities = map[string]Ability{}
	for _, a := range cf.Abilities {
		abilities[a.ID] = a
	}
	quests = map[string]Quest{}
	for _, q := range cf.Quests {
		quests[q.ID] = q
	}
	log.Printf("catalog loaded: %d abilities, %d quests\n", len(abilities), len(quests))
}

// ─── main ───────────────────────────────────────────────────────────────────
func main() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.RedirectSlashes)

	loadCatalogFromDisk()

	// Index with clickable tests
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<h3>Stream Overlay Test</h3>
<p>Connected overlays: %d</p>
<ul>
  <li><a href="/overlay">/overlay</a> (open this FIRST and leave it open)</li>
  <li><a href="/api/debug/clients">/api/debug/clients</a> (should say <b>clients: 1</b>)</li>
  <li><a href="/api/donation?donor=Alice&amount_cents=599&msg=Hello%%20world">/api/donation</a> (toast)</li>
  <li><a href="/api/ability/fire?id=goat">/api/ability/fire?id=goat</a> (auto SFX)</li>
  <li><a href="/api/quest/add?id=call-maam">/api/quest/add?id=call-maam</a> (quest)</li>
  <li><a href="/api/test/tts?text=Hello%%20Dave%%2C%%20testing%%20TTS&voice=Google">/api/test/tts</a> (browser TTS)</li>
</ul>`, clientsCount())
	})

	// Serve the overlay
	r.Get("/overlay", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(overlayHTML)
	})

	// WebSocket endpoint with keep-alive pings
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Upgrade error:", err)
			return
		}
		clientsMu.Lock()
		clients[c] = true
		total := len(clients)
		clientsMu.Unlock()
		log.Printf("ws connected (%d total)", total)

		// ping loop to keep the connection alive
		done := make(chan struct{})
		go func() {
			t := time.NewTicker(20 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-t.C:
					_ = c.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if err := c.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(10*time.Second)); err != nil {
						close(done)
						return
					}
				case <-done:
					return
				}
			}
		}()

		// read loop; refresh deadline on pong
		c.SetReadLimit(1024)
		_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))
		c.SetPongHandler(func(string) error {
			_ = c.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})

		for {
			if _, _, err := c.ReadMessage(); err != nil {
				clientsMu.Lock()
				delete(clients, c)
				total = len(clients)
				clientsMu.Unlock()
				log.Printf("ws disconnected (%d total)", total)
				_ = c.Close()
				close(done)
				return
			}
		}
	})

	// ─── Test endpoints you can hit from the browser ────────────────────────

	// Donation toast
	r.Get("/api/donation", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		amountCents := int64(0)
		if v := q.Get("amount_cents"); v != "" {
			if p, err := strconv.ParseInt(v, 10, 64); err == nil {
				amountCents = p
			}
		}
		broadcast(wsMsg{
			Type: "DONATION",
			Data: map[string]any{
				"donor":  q.Get("donor"),
				"amount": amountCents, // overlay divides by 100
				"msg":    q.Get("msg"),
			},
		})
		_, _ = w.Write([]byte("Sent donation toast"))
	})

	// Ability by ID (auto-play sound on overlay)
	r.Get("/api/ability/fire", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		a, ok := abilities[id]
		if !ok {
			http.Error(w, "unknown ability id", http.StatusNotFound)
			return
		}
		broadcast(wsMsg{
			Type: "ABILITY_FIRE",
			Data: map[string]any{"id": a.ID, "sfx_url": a.SFXURL},
		})
		_, _ = w.Write([]byte("Sent ability"))
	})

	// Quest by ID (append to quest log)
	r.Get("/api/quest/add", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		q, ok := quests[id]
		if !ok {
			http.Error(w, "unknown quest id", http.StatusNotFound)
			return
		}
		broadcast(wsMsg{
			Type: "QUEST_ADD",
			Data: map[string]any{"id": q.ID, "name": q.Name},
		})
		_, _ = w.Write([]byte("Sent quest"))
	})

	// TTS test (browser voices; your overlay must implement TTS_PLAY handling)
	r.Get("/api/test/tts", func(w http.ResponseWriter, r *http.Request) {
		text := r.URL.Query().Get("text")
		voice := r.URL.Query().Get("voice") // optional hint ("Google", "UK", "Zira", etc.)
		if text == "" {
			http.Error(w, "missing ?text=", http.StatusBadRequest)
			return
		}
		broadcast(wsMsg{
			Type: "TTS_PLAY",
			Data: map[string]any{"text": text, "voice": voice},
		})
		_, _ = w.Write([]byte("Sent TTS to overlays"))
	})

	// Serve the control panel
	r.Get("/panel", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(panelHTML)
	})

	// Catalog JSON (abilities + quests)
	r.Get("/api/catalog", func(w http.ResponseWriter, r *http.Request) {
		type cat struct {
			Abilities []Ability `json:"abilities"`
			Quests    []Quest   `json:"quests"`
		}
		abs := make([]Ability, 0, len(abilities))
		for _, a := range abilities {
			abs = append(abs, a)
		}
		qs := make([]Quest, 0, len(quests))
		for _, q := range quests {
			qs = append(qs, q)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cat{Abilities: abs, Quests: qs})
	})

	r.Post("/api/catalog/reload", func(w http.ResponseWriter, r *http.Request) {
		loadCatalogFromDisk()
		w.Write([]byte("ok"))
	})

	r.Get("/api/ability/fire", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		a, ok := abilities[id]
		if !ok {
			http.Error(w, "unknown ability id", http.StatusNotFound)
			return
		}
		broadcast(wsMsg{
			Type: "ABILITY_FIRE",
			Data: map[string]any{"id": a.ID, "name": a.Name, "sfx_url": a.SFXURL, "price_cents": a.PriceCents},
		})
		w.Write([]byte("Sent ability"))
	})

	r.Get("/api/quest/add", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		q, ok := quests[id]
		if !ok {
			http.Error(w, "unknown quest id", http.StatusNotFound)
			return
		}
		broadcast(wsMsg{
			Type: "QUEST_ADD",
			Data: map[string]any{"id": q.ID, "name": q.Name, "icon_url": q.IconURL, "price_cents": q.PriceCents},
		})
		w.Write([]byte("Sent quest"))
	})

	// Debug: count how many overlays are connected
	r.Get("/api/debug/clients", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "clients: %d\n", clientsCount())
	})

	addr := ":3000"
	log.Println("Server on http://localhost" + addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
