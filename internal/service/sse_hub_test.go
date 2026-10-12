package service

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestSSEHubBroadcastAndShutdown(t *testing.T) {
	hub := NewSSEHub(zap.NewNop())
	done := make(chan struct{})
	go func() {
		defer close(done)
		hub.Run()
	}()
	t.Cleanup(func() {
		hub.Stop()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("event hub did not stop")
		}
	})
	clients := []*ClientChannel{hub.Subscribe(), hub.Subscribe()}
	hub.Broadcast(EventTypePlayerRequestLog, "changed")
	for _, client := range clients {
		select {
		case event := <-client.Ch:
			if event.Type != EventTypePlayerRequestLog || event.Payload != "changed" {
				t.Fatalf("unexpected event: %+v", event)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive event")
		}
	}
}
