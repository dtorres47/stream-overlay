package requests

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/dtorres47/stream-overlay/internal/ws"
	"github.com/go-chi/chi/v5"
)

type RequestItem struct {
	ID          int    `json:"id"`
	Board       string `json:"board"`
	Phone       string `json:"phone"`
	MaskedPhone string `json:"masked_phone"`
	Note        string `json:"note"`
	Status      string `json:"status"`
	CreatedUnix int64  `json:"created_unix"`
}

var (
	reqMu     = sync.Mutex{}
	reqSeq    = 0
	reqQueue  = []*RequestItem{}
	reqActive = map[int]*RequestItem{}
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

func requestsListActive() []RequestItem {
	reqMu.Lock()
	defer reqMu.Unlock()
	out := make([]RequestItem, 0, len(reqActive))
	for _, it := range reqActive {
		out = append(out, *it)
	}
	return out
}

func requestFind(id int) (*RequestItem, bool) {
	reqMu.Lock()
	defer reqMu.Unlock()
	for _, it := range reqQueue {
		if it.ID == id {
			return it, true
		}
	}
	it, ok := reqActive[id]
	return it, ok
}

// RegisterRoutes mounts all /api/request/* endpoints.
func RegisterRoutes(r chi.Router) {
	r.Get("/api/request/submit", handleSubmit)
	r.Get("/api/request/queue", handleQueue)
	r.Get("/api/request/active", handleActive)
	r.Post("/api/request/approve", handleApprove)
	r.Post("/api/request/reject", handleReject)
	r.Post("/api/request/complete", handleComplete)
}

func handleSubmit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	board := strings.TrimSpace(q.Get("board"))
	phone := digitsOnly(q.Get("phone"))
	note := strings.TrimSpace(q.Get("note"))
	if board == "" && phone == "" {
		http.Error(w, "provide at least ?board= or ?phone=", http.StatusBadRequest)
		return
	}
	item := &RequestItem{
		Board:       board,
		Phone:       phone,
		MaskedPhone: maskPhone(phone),
		Note:        note,
		Status:      "pending",
		CreatedUnix: time.Now().Unix(),
	}
	reqMu.Lock()
	reqSeq++
	item.ID = reqSeq
	reqQueue = append(reqQueue, item)
	reqMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(item)
}

func handleQueue(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(requestsListPending())
}

func handleActive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(requestsListActive())
}

func handleApprove(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	it, ok := requestFind(id)
	if !ok || it.Status != "pending" {
		http.Error(w, "unknown or not pending", http.StatusNotFound)
		return
	}
	reqMu.Lock()
	it.Status = "approved"
	reqActive[id] = it
	reqMu.Unlock()

	ws.Broadcast(ws.WSMsg{
		Type: "REQUEST_ADD",
		Data: map[string]any{
			"id":           it.ID,
			"board":        it.Board,
			"masked_phone": it.MaskedPhone,
			"note":         it.Note,
		},
	})
	w.Write([]byte("ok"))
}

func handleReject(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	it, ok := requestFind(id)
	if !ok || it.Status != "pending" {
		http.Error(w, "unknown or not pending", http.StatusNotFound)
		return
	}
	reqMu.Lock()
	it.Status = "rejected"
	reqMu.Unlock()
	w.Write([]byte("ok"))
}

func handleComplete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
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
	ws.Broadcast(ws.WSMsg{Type: "REQUEST_REMOVE", Data: map[string]any{"id": id}})
	w.Write([]byte("ok"))
}

// ─────────────────────────────────────────────────────────────────────────────
// State-persistence helpers: exported so internal/state.go can call them.
// ─────────────────────────────────────────────────────────────────────────────

// GetPendingRequests returns a copy of all pending requests.
func GetPendingRequests() []*RequestItem {
	reqMu.Lock()
	defer reqMu.Unlock()
	out := make([]*RequestItem, len(reqQueue))
	copy(out, reqQueue)
	return out
}

// GetActiveRequests returns a copy of all approved/active requests.
func GetActiveRequests() []*RequestItem {
	reqMu.Lock()
	defer reqMu.Unlock()
	out := make([]*RequestItem, 0, len(reqActive))
	for _, it := range reqActive {
		out = append(out, it)
	}
	return out
}

// GetNextID returns the current request-sequence number.
func GetNextID() int {
	reqMu.Lock()
	defer reqMu.Unlock()
	return reqSeq
}

// SetState replaces in-memory request state (used by state.LoadState).
func SetState(pending []*RequestItem, active []*RequestItem, seq int) {
	reqMu.Lock()
	defer reqMu.Unlock()
	reqQueue = append([]*RequestItem{}, pending...)
	reqActive = make(map[int]*RequestItem, len(active))
	for _, it := range active {
		reqActive[it.ID] = it
	}
	reqSeq = seq
}
