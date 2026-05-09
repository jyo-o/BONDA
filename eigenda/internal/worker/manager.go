package worker

import (
	"context"
	"log"
	"sync"
)

type Worker interface {
	Name() string
	Run(ctx context.Context)
}

type Manager struct {
	workers []Worker
}

func NewManager() *Manager {
	return &Manager{}
}

func (m *Manager) Register(w Worker) {
	m.workers = append(m.workers, w)
}

func (m *Manager) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, w := range m.workers {
		wg.Add(1)
		go func(w Worker) {
			defer wg.Done()
			log.Printf("[manager] starting %s", w.Name())
			w.Run(ctx)
		}(w)
	}
	wg.Wait()
}
