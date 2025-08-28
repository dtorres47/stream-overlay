// internal/quests/quests.go
package quests

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/dtorres47/stream-overlay/internal/catalog"
	"github.com/dtorres47/stream-overlay/internal/ws"
	"github.com/go-chi/chi/v5"
)

// QuestState holds progress for an active quest.
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

// upsertQuestState creates or updates an active quest and broadcasts it.
func upsertQuestState(q catalog.Quest) *QuestState {
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
		// update any changed fields
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

	ws.Broadcast(ws.WSMsg{Type: "QUEST_UPSERT", Data: qs})
	return qs
}

// listActiveQuests returns a snapshot of all active quests.
func listActiveQuests() []QuestState {
	activeMu.Lock()
	defer activeMu.Unlock()

	out := make([]QuestState, 0, len(activeQuests))
	for _, qs := range activeQuests {
		out = append(out, *qs)
	}
	return out
}

// RegisterRoutes mounts the /api/quest/* endpoints on the given router.
func RegisterRoutes(r chi.Router) {
	// Add or upsert a quest by ID
	r.Get("/api/quest/add", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		q, ok := catalog.GetQuest(id)
		if !ok {
			http.Error(w, "unknown quest id", http.StatusNotFound)
			return
		}
		upsertQuestState(q)
		w.Write([]byte("Quest upserted"))
	})

	// List all active quests
	r.Get("/api/quest/active", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(listActiveQuests())
	})

	// Increment progress on an active quest
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
		ws.Broadcast(ws.WSMsg{Type: "QUEST_UPSERT", Data: qs})
		w.Write([]byte("ok"))
	})

	// Reset progress on an active quest
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
		ws.Broadcast(ws.WSMsg{Type: "QUEST_UPSERT", Data: qs})
		w.Write([]byte("ok"))
	})

	// Remove an active quest
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
		ws.Broadcast(ws.WSMsg{Type: "QUEST_REMOVE", Data: map[string]any{"id": id}})
		w.Write([]byte("ok"))
	})
}

// ListActiveQuests returns a snapshot of all quests (for state).
func ListActiveQuests() []QuestState { return listActiveQuests() }

// SetState replaces in-memory quest state (used by state.LoadState).
func SetState(qs []QuestState) {
	activeMu.Lock()
	defer activeMu.Unlock()
	activeQuests = make(map[string]*QuestState, len(qs))
	for _, q := range qs {
		copy := q
		activeQuests[q.ID] = &copy
	}
}
