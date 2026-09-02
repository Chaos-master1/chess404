package v1

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type EventType string

const (
	EventSearchStart EventType = "search_start"
	EventDepthDone   EventType = "depth_done"
	EventNode        EventType = "node"
	EventPrune       EventType = "prune"
	EventBestMove    EventType = "best_move"
	EventEval        EventType = "eval"
	EventInfo        EventType = "info"
)

type SearchEvent struct {
	Type      EventType `json:"type"`
	Depth     int       `json:"depth,omitempty"`
	Score     int       `json:"score,omitempty"`
	Move      string    `json:"move,omitempty"`
	Nodes     int       `json:"nodes,omitempty"`
	NPS       int       `json:"nps,omitempty"`
	Alpha     int       `json:"alpha,omitempty"`
	Beta      int       `json:"beta,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Pv        []string  `json:"pv,omitempty"`
	Timestamp int64     `json:"ts"`
}

type DashboardServer struct {
	port     int
	clients  map[*websocket.Conn]bool
	mu       sync.RWMutex
	upgrader websocket.Upgrader
	server   *http.Server
	events   []SearchEvent
	eventsMu sync.RWMutex
}

var globalDashboard *DashboardServer
var dashboardMu sync.Mutex

func StartDashboard(port int) *DashboardServer {
	dashboardMu.Lock()
	defer dashboardMu.Unlock()

	if globalDashboard != nil {
		globalDashboard.Stop()
	}

	d := &DashboardServer{
		port:    port,
		clients: make(map[*websocket.Conn]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", d.handleWS)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	d.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		log.Printf("Dashboard listening on :%d", port)
		if err := d.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Dashboard error: %v", err)
		}
	}()

	globalDashboard = d
	return d
}

func (d *DashboardServer) Stop() {
	if d.server != nil {
		d.server.Close()
	}
	d.mu.Lock()
	for conn := range d.clients {
		conn.Close()
	}
	d.clients = nil
	d.mu.Unlock()
}

func (d *DashboardServer) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := d.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	d.mu.Lock()
	d.clients[conn] = true
	d.mu.Unlock()

	// Send recent events on connect.
	d.eventsMu.RLock()
	for _, ev := range d.events {
		data, _ := json.Marshal(ev)
		conn.WriteMessage(websocket.TextMessage, data)
	}
	d.eventsMu.RUnlock()

	// Read loop (keeps connection alive).
	go func() {
		defer func() {
			d.mu.Lock()
			delete(d.clients, conn)
			d.mu.Unlock()
			conn.Close()
		}()
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}()
}

func (d *DashboardServer) broadcast(ev SearchEvent) {
	ev.Timestamp = time.Now().UnixMilli()
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}

	// Keep last 1000 events.
	d.eventsMu.Lock()
	d.events = append(d.events, ev)
	if len(d.events) > 1000 {
		d.events = d.events[len(d.events)-1000:]
	}
	d.eventsMu.Unlock()

	d.mu.RLock()
	for conn := range d.clients {
		conn.WriteMessage(websocket.TextMessage, data)
	}
	d.mu.RUnlock()
}

func EmitSearchEvent(ev SearchEvent) {
	dashboardMu.Lock()
	d := globalDashboard
	dashboardMu.Unlock()
	if d != nil {
		d.broadcast(ev)
	}
}

func EmitSearchStart(depth, nodes int) {
	EmitSearchEvent(SearchEvent{Type: EventSearchStart, Depth: depth, Nodes: nodes})
}

func EmitDepthDone(depth int, score int, pv []string, nodes int) {
	EmitSearchEvent(SearchEvent{
		Type:  EventDepthDone,
		Depth: depth,
		Score: score,
		Pv:    pv,
		Nodes: nodes,
	})
}

func EmitNode(move string, depth, score, alpha, beta int) {
	EmitSearchEvent(SearchEvent{
		Type:  EventNode,
		Move:  move,
		Depth: depth,
		Score: score,
		Alpha: alpha,
		Beta:  beta,
	})
}

func EmitPrune(move string, depth int, reason string) {
	EmitSearchEvent(SearchEvent{
		Type:   EventPrune,
		Move:   move,
		Depth:  depth,
		Reason: reason,
	})
}

func EmitBestMove(move string, score, depth int) {
	EmitSearchEvent(SearchEvent{
		Type:  EventBestMove,
		Move:  move,
		Score: score,
		Depth: depth,
	})
}

func EmitEval(score int, nodes, nps int) {
	EmitSearchEvent(SearchEvent{
		Type:  EventEval,
		Score: score,
		Nodes: nodes,
		NPS:   nps,
	})
}

func EmitInfo(msg string) {
	EmitSearchEvent(SearchEvent{
		Type:   EventInfo,
		Reason: msg,
	})
}
