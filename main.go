package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
type Ability struct{ ID, Name, SFXURL string }
type Quest struct{ ID, Name string }

var abilities = map[string]Ability{
	"goat": {ID: "goat", Name: "Play Goat Noises", SFXURL: "https://interactive-examples.mdn.mozilla.net/media/cc0-audio/t-rex-roar.mp3"},
	"trex": {ID: "trex", Name: "T-Rex Roar", SFXURL: "https://interactive-examples.mdn.mozilla.net/media/cc0-audio/t-rex-roar.mp3"},
}
var quests = map[string]Quest{
	"call-maam":     {ID: "call-maam", Name: `Call a man "ma'am"`},
	"soundboard-5x": {ID: "soundboard-5x", Name: "Make 5 calls with X soundboard"},
}

// ─── main ───────────────────────────────────────────────────────────────────
func main() {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.RedirectSlashes)

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

	// Debug: count how many overlays are connected
	r.Get("/api/debug/clients", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "clients: %d\n", clientsCount())
	})

	addr := ":3000"
	log.Println("Server on http://localhost" + addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
