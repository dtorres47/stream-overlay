package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/websocket"
)

// ─── Embeds ──────────────────────────────────────────────────────────────────
//
//go:embed overlay.html
var overlayHTML []byte

//go:embed panel.html
var panelHTML []byte

// ─── WebSocket plumbing ─────────────────────────────────────────────────────
var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

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

// ─── Catalog types & loader (file-backed) ────────────────────────────────────
type Ability struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	PriceCents int64   `json:"price_cents"`
	SFXURL     string  `json:"sfx_url"`
	IconURL    string  `json:"icon_url"`
	CooldownMs int     `json:"cooldown_ms"`
	Volume     float64 `json:"volume"`
}
type Quest struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
	IconURL    string `json:"icon_url"`
	Target     int    `json:"target"` // optional; defaults to 1
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
		abilities = map[string]Ability{
			"trex": {ID: "trex", Name: "T-Rex Roar", PriceCents: 300, SFXURL: "https://interactive-examples.mdn.mozilla.net/media/cc0-audio/t-rex-roar.mp3", CooldownMs: 3000, Volume: 0.7},
			"goat": {ID: "goat", Name: "Goat Noises", PriceCents: 200, SFXURL: "https://www.soundjay.com/buttons/sounds/button-3.mp3", CooldownMs: 2500, Volume: 0.8},
		}
		quests = map[string]Quest{
			"call-maam":     {ID: "call-maam", Name: `Call a man "ma'am"`, PriceCents: 499, Target: 1},
			"soundboard-5x": {ID: "soundboard-5x", Name: "Make 5 calls with X soundboard", PriceCents: 399, Target: 5},
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
		if q.Target <= 0 {
			q.Target = 1
		}
		quests[q.ID] = q
	}
	log.Printf("catalog loaded: %d abilities, %d quests\n", len(abilities), len(quests))
}

// ─── Quest progress state ────────────────────────────────────────────────────
type QuestState struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Target     int    `json:"target"`
	Progress   int    `json:"progress"`
	IconURL    string `json:"icon_url"`
	PriceCents int64  `json:"price_cents"`
}

var (
	activeQuests = map[string]*QuestState{}
	activeMu     sync.Mutex
)

func upsertQuestState(q Quest) *QuestState {
	activeMu.Lock()
	defer activeMu.Unlock()
	qs, ok := activeQuests[q.ID]
	if !ok {
		qs = &QuestState{
			ID:         q.ID,
			Name:       q.Name,
			Target:     q.Target,
			Progress:   0,
			IconURL:    q.IconURL,
			PriceCents: q.PriceCents,
		}
		activeQuests[q.ID] = qs
	} else {
		qs.Name = q.Name
		if q.Target > 0 {
			qs.Target = q.Target
		}
		qs.IconURL = q.IconURL
		qs.PriceCents = q.PriceCents
		if qs.Progress > qs.Target {
			qs.Progress = qs.Target
		}
	}
	broadcast(wsMsg{Type: "QUEST_UPSERT", Data: qs})
	return qs
}

func listActiveQuests() []QuestState {
	activeMu.Lock()
	defer activeMu.Unlock()
	out := make([]QuestState, 0, len(activeQuests))
	for _, qs := range activeQuests {
		out = append(out, *qs)
	}
	return out
}

// ─── TTS moderation queue ────────────────────────────────────────────────────
type TTSItem struct {
	ID          int    `json:"id"`
	Text        string `json:"text"`
	Voice       string `json:"voice"`
	Donor       string `json:"donor"`
	AmountCents int64  `json:"amount_cents"`
	Msg         string `json:"msg"`
	CreatedUnix int64  `json:"created_unix"`
	Status      string `json:"status"` // pending, approved, rejected, spoken
}

var (
	ttsMu    sync.Mutex
	ttsSeq   int
	ttsQueue = []*TTSItem{} // newest last
)

func ttsListPending() []TTSItem {
	ttsMu.Lock()
	defer ttsMu.Unlock()
	out := make([]TTSItem, 0, len(ttsQueue))
	for _, it := range ttsQueue {
		if it.Status == "pending" {
			out = append(out, *it)
		}
	}
	return out
}

func ttsFind(id int) (*TTSItem, bool) {
	ttsMu.Lock()
	defer ttsMu.Unlock()
	for _, it := range ttsQueue {
		if it.ID == id {
			return it, true
		}
	}
	return nil, false
}

// ─── Requests (board + phone) ────────────────────────────────────────────────
type RequestItem struct {
	ID          int    `json:"id"`
	Board       string `json:"board"`
	Phone       string `json:"phone"`        // full digits (panel only)
	MaskedPhone string `json:"masked_phone"` // overlay-safe
	Note        string `json:"note"`
	Status      string `json:"status"` // pending, approved, rejected, completed
	CreatedUnix int64  `json:"created_unix"`
}

var (
	reqMu     sync.Mutex
	reqSeq    int
	reqQueue  = []*RequestItem{}       // pending
	reqActive = map[int]*RequestItem{} // approved/active by id
)

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func maskPhone(d string) string {
	if d == "" {
		return ""
	}
	if len(d) == 10 {
		return fmt.Sprintf("***-***-%s", d[6:])
	}
	if len(d) == 11 && d[0] == '1' {
		return fmt.Sprintf("1-***-***-%s", d[7:])
	}
	last4 := d
	if len(d) > 4 {
		last4 = d[len(d)-4:]
	}
	return fmt.Sprintf("***-%s", last4)
}

// List pending requests (status == "pending")
func requestsListPending() []RequestItem {
	reqMu.Lock()
	defer reqMu.Unlock()
	out := make([]RequestItem, 0, len(reqQueue))
	for _, it := range reqQueue {
		if it.Status == "pending" {
			out = append(out, *it)
		}
	}
	return out
}

// List active (approved) requests
func requestsListActive() []RequestItem {
	reqMu.Lock()
	defer reqMu.Unlock()
	out := make([]RequestItem, 0, len(reqActive))
	for _, it := range reqActive {
		out = append(out, *it)
	}
	return out
}

// Find a request by id in either pending queue or active map
func requestFind(id int) (*RequestItem, bool) {
	reqMu.Lock()
	defer reqMu.Unlock()
	for _, it := range reqQueue {
		if it.ID == id {
			return it, true
		}
	}
	if it, ok := reqActive[id]; ok {
		return it, true
	}
	return nil, false
}

// ─── Persistent state (save/restore) ────────────────────────────────────────
type PersistState struct {
	ActiveQuests    []QuestState   `json:"active_quests"`
	RequestsPending []*RequestItem `json:"requests_pending"`
	RequestsActive  []*RequestItem `json:"requests_active"`
	TTSQueue        []*TTSItem     `json:"tts_queue"`
	ReqSeq          int            `json:"req_seq"`
	TTSSeq          int            `json:"tts_seq"`
	SavedAtUnix     int64          `json:"saved_at_unix"`
}

func saveState() {
	ps := PersistState{SavedAtUnix: time.Now().Unix()}
	// copy active quests
	ps.ActiveQuests = listActiveQuests()
	// copy requests
	reqMu.Lock()
	ps.RequestsPending = make([]*RequestItem, len(reqQueue))
	copy(ps.RequestsPending, reqQueue)
	ps.RequestsActive = make([]*RequestItem, 0, len(reqActive))
	for _, it := range reqActive {
		ps.RequestsActive = append(ps.RequestsActive, it)
	}
	ps.ReqSeq = reqSeq
	reqMu.Unlock()
	// copy TTS
	ttsMu.Lock()
	ps.TTSQueue = make([]*TTSItem, len(ttsQueue))
	copy(ps.TTSQueue, ttsQueue)
	ps.TTSSeq = ttsSeq
	ttsMu.Unlock()

	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		log.Println("state marshal error:", err)
		return
	}
	tmp := "state.json.tmp"
	final := "state.json"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		log.Println("state write error:", err)
		return
	}
	if err := os.Rename(tmp, final); err != nil {
		log.Println("state rename error:", err)
		return
	}
	log.Printf("state saved: %d quests, %d pending req, %d active req, %d tts\n",
		len(ps.ActiveQuests), len(ps.RequestsPending), len(ps.RequestsActive), len(ps.TTSQueue))
}

func loadState() {
	b, err := os.ReadFile("state.json")
	if err != nil {
		return // no prior state
	}
	var ps PersistState
	if err := json.Unmarshal(b, &ps); err != nil {
		log.Println("state parse error:", err)
		return
	}
	// restore active quests
	activeMu.Lock()
	activeQuests = map[string]*QuestState{}
	for _, qs := range ps.ActiveQuests {
		qc := qs
		activeQuests[qs.ID] = &qc
	}
	activeMu.Unlock()
	// restore requests
	reqMu.Lock()
	reqQueue = []*RequestItem{}
	for _, it := range ps.RequestsPending {
		reqQueue = append(reqQueue, it)
	}
	reqActive = map[int]*RequestItem{}
	for _, it := range ps.RequestsActive {
		reqActive[it.ID] = it
	}
	reqSeq = ps.ReqSeq
	reqMu.Unlock()
	// restore TTS
	ttsMu.Lock()
	ttsQueue = []*TTSItem{}
	for _, it := range ps.TTSQueue {
		ttsQueue = append(ttsQueue, it)
	}
	ttsSeq = ps.TTSSeq
	ttsMu.Unlock()

	log.Printf("state loaded: %d quests, %d pending req, %d active req, %d tts\n",
		len(ps.ActiveQuests), len(ps.RequestsPending), len(ps.RequestsActive), len(ps.TTSQueue))
}

// rebroadcast current visible state to overlay (after reconnect or restart)
func rebroadcastOverlay() {
	// quests
	for _, qs := range listActiveQuests() {
		qsCopy := qs
		broadcast(wsMsg{Type: "QUEST_UPSERT", Data: &qsCopy})
	}
	// requests (masked)
	reqMu.Lock()
	for _, it := range reqActive {
		broadcast(wsMsg{
			Type: "REQUEST_ADD",
			Data: map[string]any{
				"id":           it.ID,
				"board":        it.Board,
				"masked_phone": it.MaskedPhone,
				"note":         it.Note,
			},
		})
	}
	reqMu.Unlock()
}

// ─── Server setup ────────────────────────────────────────────────────────────
func main() {
	loadCatalogFromDisk()
	loadState() // <- restore prior session if present

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.RedirectSlashes)

	// Index
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<h3>Stream Overlay</h3>
<p>Connected overlays: %d</p>
<ul>
  <li><a href="/overlay" target="_blank">/overlay</a></li>
  <li><a href="/panel" target="_blank">/panel</a></li>
  <li><a href="/api/debug/clients" target="_blank">/api/debug/clients</a></li>
</ul>`, clientsCount())
	})

	// Overlay + WS
	r.Get("/overlay", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(overlayHTML)
	})
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Upgrade:", err)
			return
		}
		clientsMu.Lock()
		clients[c] = true
		total := len(clients)
		clientsMu.Unlock()
		log.Printf("ws connected (%d total)", total)

		// keepalive pings
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

		// read loop w/ pong
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

	// Panel + catalog
	r.Get("/panel", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(panelHTML)
	})
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

	// Debug/state helpers
	r.Get("/api/debug/clients", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "clients: %d\n", clientsCount())
	})
	r.Post("/api/state/save", func(w http.ResponseWriter, r *http.Request) {
		saveState()
		w.Write([]byte("ok"))
	})
	r.Post("/api/state/rehydrate", func(w http.ResponseWriter, r *http.Request) {
		rebroadcastOverlay()
		w.Write([]byte("ok"))
	})

	// Donation toast (manual)
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
				"amount": amountCents,
				"msg":    q.Get("msg"),
			},
		})
		w.Write([]byte("Sent donation toast"))
	})

	// Ability (auto SFX)
	r.Get("/api/ability/fire", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		a, ok := abilities[id]
		if !ok {
			http.Error(w, "unknown ability id", http.StatusNotFound)
			return
		}
		broadcast(wsMsg{
			Type: "ABILITY_FIRE",
			Data: map[string]any{
				"id":          a.ID,
				"name":        a.Name,
				"sfx_url":     a.SFXURL,
				"price_cents": a.PriceCents,
				"cooldown_ms": a.CooldownMs,
				"volume":      a.Volume,
			},
		})
		w.Write([]byte("Sent ability"))
	})

	// TTS (direct test)
	r.Get("/api/test/tts", func(w http.ResponseWriter, r *http.Request) {
		text := r.URL.Query().Get("text")
		voice := r.URL.Query().Get("voice")
		if text == "" {
			http.Error(w, "missing ?text=", http.StatusBadRequest)
			return
		}
		broadcast(wsMsg{Type: "TTS_PLAY", Data: map[string]any{"text": text, "voice": voice}})
		w.Write([]byte("Sent TTS"))
	})

	// ─── Quest endpoints ─────────────────────────────────────────────────────
	r.Get("/api/quest/add", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		q, ok := quests[id]
		if !ok {
			http.Error(w, "unknown quest id", http.StatusNotFound)
			return
		}
		if q.Target <= 0 {
			q.Target = 1
		}
		upsertQuestState(q)
		saveState()
		w.Write([]byte("Quest upserted"))
	})
	r.Get("/api/quest/active", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listActiveQuests())
	})
	r.Post("/api/quest/inc", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		activeMu.Lock()
		qs, ok := activeQuests[id]
		if ok && qs.Progress < qs.Target {
			qs.Progress++
		}
		activeMu.Unlock()
		if !ok {
			http.Error(w, "unknown active quest id", http.StatusNotFound)
			return
		}
		broadcast(wsMsg{Type: "QUEST_UPSERT", Data: qs})
		saveState()
		w.Write([]byte("ok"))
	})
	r.Post("/api/quest/reset", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		activeMu.Lock()
		qs, ok := activeQuests[id]
		if ok {
			qs.Progress = 0
		}
		activeMu.Unlock()
		if !ok {
			http.Error(w, "unknown active quest id", http.StatusNotFound)
			return
		}
		broadcast(wsMsg{Type: "QUEST_UPSERT", Data: qs})
		saveState()
		w.Write([]byte("ok"))
	})
	r.Post("/api/quest/remove", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		activeMu.Lock()
		_, ok := activeQuests[id]
		if ok {
			delete(activeQuests, id)
		}
		activeMu.Unlock()
		if !ok {
			http.Error(w, "unknown active quest id", http.StatusNotFound)
			return
		}
		broadcast(wsMsg{Type: "QUEST_REMOVE", Data: map[string]any{"id": id}})
		saveState()
		w.Write([]byte("ok"))
	})

	// ─── TTS moderation queue ────────────────────────────────────────────────
	r.Get("/api/tts/submit", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		text := q.Get("text")
		if text == "" {
			http.Error(w, "missing ?text=", http.StatusBadRequest)
			return
		}
		voice := q.Get("voice")
		donor := q.Get("donor")
		msg := q.Get("msg")
		var amt int64
		if v := q.Get("amount_cents"); v != "" {
			if p, err := strconv.ParseInt(v, 10, 64); err == nil {
				amt = p
			}
		}
		ttsMu.Lock()
		ttsSeq++
		item := &TTSItem{
			ID:          ttsSeq,
			Text:        text,
			Voice:       voice,
			Donor:       donor,
			AmountCents: amt,
			Msg:         msg,
			CreatedUnix: time.Now().Unix(),
			Status:      "pending",
		}
		ttsQueue = append(ttsQueue, item)
		ttsMu.Unlock()
		saveState()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(item)
	})
	r.Get("/api/tts/queue", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ttsListPending())
	})
	r.Post("/api/tts/approve", func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("id")
		id, _ := strconv.Atoi(idStr)
		it, ok := ttsFind(id)
		if !ok || it.Status != "pending" {
			http.Error(w, "unknown or not pending", http.StatusNotFound)
			return
		}
		ttsMu.Lock()
		it.Status = "approved"
		ttsMu.Unlock()

		if it.Donor != "" || it.AmountCents > 0 || it.Msg != "" {
			broadcast(wsMsg{
				Type: "DONATION",
				Data: map[string]any{
					"donor":  it.Donor,
					"amount": it.AmountCents,
					"msg":    it.Msg,
				},
			})
		}
		broadcast(wsMsg{Type: "TTS_PLAY", Data: map[string]any{"text": it.Text, "voice": it.Voice}})

		ttsMu.Lock()
		it.Status = "spoken"
		ttsMu.Unlock()
		saveState()
		w.Write([]byte("ok"))
	})
	r.Post("/api/tts/reject", func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("id")
		id, _ := strconv.Atoi(idStr)
		it, ok := ttsFind(id)
		if !ok || it.Status != "pending" {
			http.Error(w, "unknown or not pending", http.StatusNotFound)
			return
		}
		ttsMu.Lock()
		it.Status = "rejected"
		ttsMu.Unlock()
		saveState()
		w.Write([]byte("ok"))
	})

	// ─── Requests: submit → moderate → overlay ───────────────────────────────
	r.Get("/api/request/submit", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		board := strings.TrimSpace(q.Get("board"))
		phoneDigits := digitsOnly(q.Get("phone"))
		note := strings.TrimSpace(q.Get("note"))
		if board == "" && phoneDigits == "" {
			http.Error(w, "provide at least ?board= or ?phone=", http.StatusBadRequest)
			return
		}
		item := &RequestItem{
			Board:       board,
			Phone:       phoneDigits,
			MaskedPhone: maskPhone(phoneDigits),
			Note:        note,
			Status:      "pending",
			CreatedUnix: time.Now().Unix(),
		}
		reqMu.Lock()
		reqSeq++
		item.ID = reqSeq
		reqQueue = append(reqQueue, item)
		reqMu.Unlock()
		saveState()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(item)
	})
	r.Get("/api/request/queue", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		out := requestsListPending()
		_ = json.NewEncoder(w).Encode(out)
	})
	r.Get("/api/request/active", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		out := requestsListActive()
		_ = json.NewEncoder(w).Encode(out)
	})
	r.Post("/api/request/approve", func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("id")
		id, _ := strconv.Atoi(idStr)
		it, ok := requestFind(id)
		if !ok || it.Status != "pending" {
			http.Error(w, "unknown or not pending", http.StatusNotFound)
			return
		}
		reqMu.Lock()
		it.Status = "approved"
		reqActive[it.ID] = it
		reqMu.Unlock()
		saveState()
		broadcast(wsMsg{
			Type: "REQUEST_ADD",
			Data: map[string]any{
				"id":           it.ID,
				"board":        it.Board,
				"masked_phone": it.MaskedPhone,
				"note":         it.Note,
			},
		})
		w.Write([]byte("ok"))
	})
	r.Post("/api/request/reject", func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("id")
		id, _ := strconv.Atoi(idStr)
		it, ok := requestFind(id)
		if !ok || it.Status != "pending" {
			http.Error(w, "unknown or not pending", http.StatusNotFound)
			return
		}
		reqMu.Lock()
		it.Status = "rejected"
		reqMu.Unlock()
		saveState()
		w.Write([]byte("ok"))
	})
	r.Post("/api/request/complete", func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Query().Get("id")
		id, _ := strconv.Atoi(idStr)
		reqMu.Lock()
		_, ok := reqActive[id]
		if ok {
			delete(reqActive, id)
		}
		reqMu.Unlock()
		if !ok {
			http.Error(w, "unknown active request id", http.StatusNotFound)
			return
		}
		saveState()
		broadcast(wsMsg{Type: "REQUEST_REMOVE", Data: map[string]any{"id": id}})
		w.Write([]byte("ok"))
	})

	addr := ":3000"
	log.Println("Server on http://localhost" + addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
