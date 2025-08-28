package state

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dtorres47/stream-overlay/internal/quests"
	"github.com/dtorres47/stream-overlay/internal/requests"
	"github.com/dtorres47/stream-overlay/internal/tts"
	"github.com/dtorres47/stream-overlay/internal/ws"

	"github.com/go-chi/chi/v5"
)

type PersistState struct {
	ActiveQuests    []quests.QuestState     `json:"active_quests"`
	RequestsPending []*requests.RequestItem `json:"requests_pending"`
	RequestsActive  []*requests.RequestItem `json:"requests_active"`
	TTSQueue        []*tts.TTSItem          `json:"tts_queue"`
	ReqSeq          int                     `json:"req_seq"`
	TTSSeq          int                     `json:"tts_seq"`
	SavedAtUnix     int64                   `json:"saved_at_unix"`
}

func SaveState() {
	ps := PersistState{SavedAtUnix: time.Now().Unix()}

	// snapshot quests
	ps.ActiveQuests = quests.ListActiveQuests()

	// snapshot requests
	ps.RequestsPending = requests.GetPendingRequests()
	ps.RequestsActive = requests.GetActiveRequests()
	ps.ReqSeq = requests.GetNextID()

	// snapshot TTS
	ps.TTSQueue = tts.GetQueue()
	ps.TTSSeq = tts.GetNextID()

	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		log.Println("state marshal error:", err)
		return
	}
	if err := os.WriteFile("state.json", b, 0644); err != nil {
		log.Println("state write error:", err)
		return
	}
	log.Printf("state saved")
}

func LoadState() {
	b, err := os.ReadFile("state.json")
	if err != nil {
		return // nothing to restore
	}
	var ps PersistState
	if err := json.Unmarshal(b, &ps); err != nil {
		log.Println("state parse error:", err)
		return
	}

	// restore quests
	quests.SetState(ps.ActiveQuests)

	// restore requests
	requests.SetState(ps.RequestsPending, ps.RequestsActive, ps.ReqSeq)

	// restore TTS
	tts.SetState(ps.TTSQueue, ps.TTSSeq)

	log.Printf("state loaded")
}

func RegisterRoutes(r chi.Router) {
	r.Post("/api/state/save", func(w http.ResponseWriter, r *http.Request) {
		SaveState()
		w.Write([]byte("ok"))
	})
	r.Post("/api/state/rehydrate", func(w http.ResponseWriter, r *http.Request) {
		// rebroadcast quests
		for _, qs := range quests.ListActiveQuests() {
			ws.Broadcast(ws.WSMsg{Type: "QUEST_UPSERT", Data: qs})
		}
		// rebroadcast requests
		for _, it := range requests.GetActiveRequests() {
			ws.Broadcast(ws.WSMsg{Type: "REQUEST_ADD", Data: map[string]any{
				"id":           it.ID,
				"board":        it.Board,
				"masked_phone": it.MaskedPhone,
				"note":         it.Note,
			}})
		}
		// rebroadcast TTS if desired (omitted for brevity)
		w.Write([]byte("ok"))
	})
}
