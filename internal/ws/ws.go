package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

var (
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
)

type WSMsg struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

func Broadcast(m WSMsg) int {
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
	log.Printf("Broadcast %q to %d client(s)", m.Type, n)
	return n
}

func ClientsCount() int {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	return len(clients)
}

func WSHandler(w http.ResponseWriter, r *http.Request) {
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
}
