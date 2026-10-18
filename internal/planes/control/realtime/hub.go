// Copyright 2026 Sarvex Lab
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const writeTimeout = 5 * time.Second

// Client wraps a single WebSocket connection. gorilla/websocket forbids
// concurrent writes to the same connection from multiple goroutines, so every
// write (whether from Broadcast or a handler-owned ping loop) must go through
// the same mutex.
type Client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

// NewClient wraps an upgraded WebSocket connection for registration with a Hub.
func NewClient(conn *websocket.Conn) *Client {
	return &Client{conn: conn}
}

func (c *Client) writeText(message []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.conn.WriteMessage(websocket.TextMessage, message)
}

// Hub fans out journey.updated messages to every Client watching a store_id.
// One process-wide instance is shared across all WebSocket connections the
// control-plane instance is currently holding open.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*Client]struct{} // keyed by store_id
}

// NewHub constructs an empty Hub.
func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[*Client]struct{})}
}

// Register adds client to the set watching storeID. The caller owns the
// connection's lifecycle and must call Unregister when the connection closes.
func (h *Hub) Register(storeID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[storeID] == nil {
		h.clients[storeID] = make(map[*Client]struct{})
	}
	h.clients[storeID][client] = struct{}{}
}

// Unregister removes client from storeID's watch set. Safe to call more than
// once for the same client.
func (h *Hub) Unregister(storeID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set, ok := h.clients[storeID]
	if !ok {
		return
	}
	delete(set, client)
	if len(set) == 0 {
		delete(h.clients, storeID)
	}
}

// Broadcast pushes message to every client currently watching storeID. A
// client whose write fails (closed, dead connection) is dropped from the
// watch set — the handler's read loop will also be unblocking around the
// same time and will call Unregister itself, so this is a defensive second
// removal rather than the primary cleanup path.
func (h *Hub) Broadcast(storeID string, message []byte) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients[storeID]))
	for c := range h.clients[storeID] {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		if err := c.writeText(message); err != nil {
			h.Unregister(storeID, c)
		}
	}
}
