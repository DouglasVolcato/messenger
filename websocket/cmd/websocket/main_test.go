package main

import (
	"bytes"
	"testing"
)

func TestSocketServerDeliverTargetsOnlyMatchingLocalConnections(t *testing.T) {
	server := &socketServer{
		clients: make(map[string]*Client),
		byUser:  make(map[string]map[string]*Client),
	}

	newClient := func(userID, connectionID string) *Client {
		return &Client{
			userID:       userID,
			connectionID: connectionID,
			send:         make(chan []byte, 1),
			done:         make(chan struct{}),
			server:       server,
		}
	}

	first := newClient("user-a", "connection-a1")
	second := newClient("user-a", "connection-a2")
	other := newClient("user-b", "connection-b1")
	server.add(first)
	server.add(second)
	server.add(other)

	payload := []byte(`{"id":"notification-1"}`)
	if delivered := server.deliver("user-a", payload); delivered != 2 {
		t.Fatalf("expected 2 local deliveries, got %d", delivered)
	}

	for _, client := range []*Client{first, second} {
		select {
		case got := <-client.send:
			if !bytes.Equal(got, payload) {
				t.Fatalf("unexpected payload: %s", got)
			}
		default:
			t.Fatalf("expected notification for %s", client.connectionID)
		}
	}

	select {
	case got := <-other.send:
		t.Fatalf("unexpected notification for unrelated user: %s", got)
	default:
	}
}
