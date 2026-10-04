package catalogue

import (
	"strings"
	"sync"
)

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
	s.services = make(map[string]Service, len(services))
	for _, svc := range services {
		s.services[svc.Metadata.Name] = svc
	}
}

func (s *Store) Get(name string) (Service, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[name]
	return svc, ok
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

func (s *Store) Search(query, tier, lifecycle string) []Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := strings.ToLower(query)
	var out []Service
	for _, svc := range s.services {
		if tier != "" && svc.Metadata.Tier != tier {
			continue
		}
		if lifecycle != "" && svc.Metadata.Lifecycle != lifecycle {
			continue
		}
		if q != "" {
			match := strings.Contains(strings.ToLower(svc.Metadata.Name), q) ||
				strings.Contains(strings.ToLower(svc.Metadata.Owner), q) ||
				strings.Contains(strings.ToLower(svc.Spec.Language), q)
			if !match {
				for _, tag := range svc.Metadata.Tags {
					if strings.Contains(strings.ToLower(tag), q) {
						match = true
						break
					}
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, svc)
	}
	return out
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.services)
}
