package server

import "sync"

// Broadcaster is an in-memory fan-out pub/sub for SSE clients.
type Broadcaster struct {
	mu   sync.RWMutex
	subs map[chan []byte]struct{}
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: make(map[chan []byte]struct{})}
}

func (b *Broadcaster) Subscribe() chan []byte {
	ch := make(chan []byte, 8)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broadcaster) Unsubscribe(ch chan []byte) {
	b.mu.Lock()
	delete(b.subs, ch)
	close(ch)
	b.mu.Unlock()
}

// Publish sends data to all subscribers; slow consumers are dropped.
func (b *Broadcaster) Publish(data []byte) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- data:
		default:
		}
	}
}
