package main

import (
	"strconv"
	"testing"
	"time"
)

func TestParseSessionsFiltersExpiredAndMalformedEntries(t *testing.T) {
	now := time.Now().Unix()
	values := map[string]string{
		"active":    "replica-a:" + strconv.FormatInt(now+60, 10),
		"expired":   "replica-b:" + strconv.FormatInt(now-1, 10),
		"malformed": "missing-expiration",
	}

	sessions, expired := parseSessions(values)
	if len(sessions) != 1 {
		t.Fatalf("expected one active session, got %d", len(sessions))
	}
	if sessions[0].ConnectionID != "active" || sessions[0].ReplicaID != "replica-a" {
		t.Fatalf("unexpected active session: %#v", sessions[0])
	}
	if len(expired) != 2 {
		t.Fatalf("expected two invalid/expired connection IDs, got %d", len(expired))
	}
}
