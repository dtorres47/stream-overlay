package tts

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/dtorres47/stream-overlay/internal/ws"
	"github.com/go-chi/chi/v5"
)

type TTSItem struct {
	ID          int    `json:"id"`
	Text        string `json:"text"`
	Voice       string `json:"voice"`
	Donor       string `json:"donor"`
	AmountCents int64  `json:"amount_cents"`
	Msg         string `json:"msg"`
	CreatedUnix int64  `json:"created_unix"`
	Status      string `json:"status"`
}

var (
	ttsMu    = sync.Mutex{}
	ttsSeq   = 0
	ttsQueue = []*TTSItem{}
)

func ttsListPending() []TTSItem {
	ttsMu.Lock()
	defer ttsMu.Unlock()
	out := []TTSItem{}
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

func RegisterRoutes(r *chi.Mux) {
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
		item := &TTSItem{ID: ttsSeq, Text: text, Voice: voice, Donor: donor, AmountCents: amt, Msg: msg, CreatedUnix: time.Now().Unix(), Status: "pending"}
		ttsQueue = append(ttsQueue, item)
		ttsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(item)
	})

	r.Get("/api/tts/queue", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ttsListPending())
	})

	r.Post("/api/tts/approve", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.URL.Query().Get("id"))
		it, ok := ttsFind(id)
		if !ok || it.Status != "pending" {
			http.Error(w, "unknown or not pending", http.StatusNotFound)
			return
		}
		ttsMu.Lock()
		it.Status = "approved"
		ttsMu.Unlock()
		if it.Donor != "" || it.AmountCents > 0 || it.Msg != "" {
			ws.Broadcast(ws.WSMsg{Type: "DONATION", Data: map[string]any{"donor": it.Donor, "amount": it.AmountCents, "msg": it.Msg}})
		}
		ws.Broadcast(ws.WSMsg{Type: "TTS_PLAY", Data: map[string]any{"text": it.Text, "voice": it.Voice}})
		ttsMu.Lock()
		it.Status = "spoken"
		ttsMu.Unlock()
		w.Write([]byte("ok"))
	})

	r.Post("/api/tts/reject", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.URL.Query().Get("id"))
		it, ok := ttsFind(id)
		if !ok || it.Status != "pending" {
			http.Error(w, "unknown or not pending", http.StatusNotFound)
			return
		}
		ttsMu.Lock()
		it.Status = "rejected"
		ttsMu.Unlock()
		w.Write([]byte("ok"))
	})
}

// GetQueue returns a copy of the current TTS queue (all items, in order).
func GetQueue() []*TTSItem {
	ttsMu.Lock()
	defer ttsMu.Unlock()
	// copy the slice to avoid mutation
	out := make([]*TTSItem, len(ttsQueue))
	copy(out, ttsQueue)
	return out
}

// GetNextID returns the current TTS sequence counter.
func GetNextID() int {
	ttsMu.Lock()
	defer ttsMu.Unlock()
	return ttsSeq
}

// SetState replaces the in-memory TTS queue and sequence counter.
// Used by state.LoadState to restore a saved session.
func SetState(queue []*TTSItem, seq int) {
	ttsMu.Lock()
	defer ttsMu.Unlock()
	// copy incoming slice to avoid aliasing
	ttsQueue = make([]*TTSItem, len(queue))
	copy(ttsQueue, queue)
	ttsSeq = seq
}
