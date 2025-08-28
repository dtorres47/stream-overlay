package catalog

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

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

func LoadCatalogFromDisk() {
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

func RegisterRoutes(r *chi.Mux) {
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
		LoadCatalogFromDisk()
		w.Write([]byte("ok"))
	})
}

// GetQuest returns the Quest with the given ID, and whether it exists.
func GetQuest(id string) (Quest, bool) {
	q, ok := quests[id]
	return q, ok
}
