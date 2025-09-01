package history

import (
	"encoding/json"
	"net/http"
	"os"
	"time"
)

// RecordDonation appends each incoming donation to web/data/donations.json
func RecordDonation(w http.ResponseWriter, r *http.Request) {
	// expected payload
	var d struct {
		Time    time.Time `json:"time"`
		Donor   string    `json:"donor"`
		Amount  float64   `json:"amount"`
		Message string    `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	// path relative to your working dir
	file := "cmd/stream-overlay/web/data/donations.json"
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		http.Error(w, "cannot open data file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// decode existing array
	var arr []interface{}
	json.NewDecoder(f).Decode(&arr)

	// append new donation
	arr = append(arr, d)

	// rewind & rewrite
	f.Truncate(0)
	f.Seek(0, 0)
	json.NewEncoder(f).Encode(arr)

	w.WriteHeader(http.StatusNoContent)
}
