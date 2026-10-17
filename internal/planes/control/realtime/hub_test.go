// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package realtime

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var testLog = slog.New(slog.NewTextHandler(io.Discard, nil))

// dialClient upgrades a test HTTP server connection to a WebSocket and wraps
// it as a *Client, returning both the Client (for Hub registration) and the
// raw *websocket.Conn (for reading what the Hub broadcasts).
func dialClient(t *testing.T, srv *httptest.Server) (*Client, *websocket.Conn) {
	t.Helper()
	url := "ws" + srv.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return NewClient(conn), conn
}

func newUpgradeServer(t *testing.T, hub *Hub, storeID string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := NewClient(conn)
		hub.Register(storeID, client)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHub_Broadcast_DeliversToRegisteredStore(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	_, conn := dialClient(t, srv)

	// Give the server goroutine a moment to register before broadcasting.
	time.Sleep(20 * time.Millisecond)
	hub.Broadcast("store-1", []byte(`{"type":"journey.updated"}`))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected a message, got error: %v", err)
	}
	if string(msg) != `{"type":"journey.updated"}` {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestHub_Broadcast_DoesNotCrossStores(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	_, conn := dialClient(t, srv)

	time.Sleep(20 * time.Millisecond)
	hub.Broadcast("store-2", []byte(`{"type":"journey.updated"}`))

	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Error("expected no message for an unregistered store, got one")
	}
}

func TestHub_Unregister_StopsDelivery(t *testing.T) {
	hub := NewHub()
	client := NewClient(nil) // never written to below; identity-only for map membership
	hub.Register("store-1", client)
	hub.Unregister("store-1", client)

	hub.mu.RLock()
	_, stillPresent := hub.clients["store-1"]
	hub.mu.RUnlock()
	if stillPresent {
		t.Error("expected store-1's client set to be removed once empty")
	}
}

func TestHub_Broadcast_DropsDeadClientOnWriteFailure(t *testing.T) {
	hub := NewHub()
	srv := newUpgradeServer(t, hub, "store-1")
	client, conn := dialClient(t, srv)
	time.Sleep(20 * time.Millisecond)

	_ = conn.Close() // close the client side; server-side write should now fail

	// First broadcast should observe the failure and unregister the client.
	for i := 0; i < 10; i++ {
		hub.Broadcast("store-1", []byte("x"))
		hub.mu.RLock()
		_, present := hub.clients["store-1"]
		hub.mu.RUnlock()
		if !present {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = client
	t.Error("expected the dead client to be unregistered after a failed write")
}
