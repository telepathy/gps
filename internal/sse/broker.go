package sse

import (
	"gps/internal/model"
	"sync"
)

// Broker manages SSE subscribers per plan. It is independent of the
// data store so it can be shared between mock and MySQL implementations.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan model.SSEEvent]bool
}

func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[string]map[chan model.SSEEvent]bool),
	}
}

func (b *Broker) Subscribe(planID string) chan model.SSEEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan model.SSEEvent, 100)
	if b.subscribers[planID] == nil {
		b.subscribers[planID] = make(map[chan model.SSEEvent]bool)
	}
	b.subscribers[planID][ch] = true
	return ch
}

func (b *Broker) Unsubscribe(planID string, ch chan model.SSEEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if subs, ok := b.subscribers[planID]; ok {
		delete(subs, ch)
		close(ch)
	}
}

func (b *Broker) Broadcast(planID string, event model.SSEEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if subs, ok := b.subscribers[planID]; ok {
		for ch := range subs {
			select {
			case ch <- event:
			default:
				// Drop if buffer full
			}
		}
	}
}
