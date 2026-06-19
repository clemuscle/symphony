package catalog

import "sync"

type Store struct {
	mu       sync.RWMutex
	services map[string]Service
}

func NewStore() *Store {
	return &Store{services: make(map[string]Service)}
}

func (s *Store) Replace(services []Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.services = make(map[string]Service)
	for _, svc := range services {
		s.services[svc.Metadata.Name] = svc
	}
}

func (s *Store) All() []Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Service, 0, len(s.services))
	for _, svc := range s.services {
		out = append(out, svc)
	}
	return out
}

func (s *Store) Get(name string) (Service, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[name]
	return svc, ok
}
