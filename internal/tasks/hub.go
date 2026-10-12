package tasks

import (
	"sync"

	"github.com/clemuscle/symphony/internal/database"
)

// Hub diffuse chaque changement de tâche à ses abonnés (un par connexion SSE)
type Hub struct {
	mu   sync.Mutex
	subs map[chan database.Task]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan database.Task]struct{}{}} }

// Subscribe renvoie un canal d'événements et la fonction qui s'en désabonne.
// Un abonné trop lent pour suivre est déconnecté (canal fermé) : le client se
// reconnecte et relit la liste, plutôt que de rater des événements sans le savoir.
func (h *Hub) Subscribe() (<-chan database.Task, func()) {
	ch := make(chan database.Task, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

func (h *Hub) Publish(t database.Task) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- t:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}
