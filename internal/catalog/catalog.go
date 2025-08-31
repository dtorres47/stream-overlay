package catalog

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

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
	Target     int    `json:"target"`
}

var (
	abilities = map[string]Ability{}
	quests    = map[string]Quest{}
)

type catalogFile struct {
	Abilities []Ability `json:"abilities"`
	Quests    []Quest   `json:"quests"`
}

// LoadCatalog unmarshals the embedded catalog.json into memory.
func LoadCatalog() {
	var cf catalogFile
	if err := json.Unmarshal(embeddedCatalog, &cf); err != nil {
		log.Fatalf("failed to parse embedded catalog: %v", err)
	}

	abilities = make(map[string]Ability, len(cf.Abilities))
	for _, a := range cf.Abilities {
		abilities[a.ID] = a
	}

	quests = make(map[string]Quest, len(cf.Quests))
	for _, q := range cf.Quests {
		if q.Target <= 0 {
			q.Target = 1
		}
		quests[q.ID] = q
	}

	log.Printf("catalog loaded: %d abilities, %d quests\n", len(abilities), len(quests))
}

// GetQuest returns the Quest with the given ID.
func GetQuest(id string) (Quest, bool) {
	q, ok := quests[id]
	return q, ok
}

// RegisterRoutes mounts the /api/catalog endpoints.
func RegisterRoutes(r *chi.Mux) {
	// GET /api/catalog
	r.Get("/api/catalog", func(w http.ResponseWriter, r *http.Request) {
		type resp struct {
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
		_ = json.NewEncoder(w).Encode(resp{Abilities: abs, Quests: qs})
	})
}
