package relay

import (
	"context"
	"sync"
)

// MemoryMessageBus is a channel-based in-memory pub/sub for single-instance or testing.
type MemoryMessageBus struct {
	mu   sync.RWMutex
	subs map[string][]memSub
}

type memSub struct {
	ch     chan []byte
	cancel func()
}

// NewMemoryMessageBus creates a new in-memory message bus.
func NewMemoryMessageBus() *MemoryMessageBus {
	return &MemoryMessageBus{
		subs: make(map[string][]memSub),
	}
}

func (b *MemoryMessageBus) Publish(_ context.Context, channel string, msg []byte) error {
	b.mu.RLock()
	subs := b.subs[channel]
	b.mu.RUnlock()

	for _, s := range subs {
		select {
		case s.ch <- msg:
		default:
			// drop on slow consumer
		}
	}
	return nil
}

func (b *MemoryMessageBus) Subscribe(_ context.Context, channel string) (<-chan []byte, func(), error) {
	ch := make(chan []byte, 64)
	done := make(chan struct{})

	unsub := func() {
		select {
		case <-done:
			return
		default:
			close(done)
		}

		b.mu.Lock()
		subs := b.subs[channel]
		for i, s := range subs {
			if s.ch == ch {
				b.subs[channel] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
		close(ch)
	}

	b.mu.Lock()
	b.subs[channel] = append(b.subs[channel], memSub{ch: ch, cancel: unsub})
	b.mu.Unlock()

	return ch, unsub, nil
}
