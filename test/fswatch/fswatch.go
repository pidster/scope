package fswatch

import (
	"sync"

	"github.com/fsnotify/fsnotify"
)

func NewMockWatcher() *MockWatcher {
	return &MockWatcher{
		events:  make(chan fsnotify.Event),
		errors:  make(chan error),
		watched: map[string]struct{}{},
	}
}

type MockWatcher struct {
	events  chan fsnotify.Event
	errors  chan error
	watched map[string]struct{}
	sync.Mutex
}

func (w *MockWatcher) Events() chan fsnotify.Event { return w.events }
func (w *MockWatcher) Errors() chan error          { return w.errors }

func (w *MockWatcher) Add(path string) error {
	w.Lock()
	w.watched[path] = struct{}{}
	w.Unlock()
	return nil
}

func (w *MockWatcher) Remove(path string) error {
	w.Lock()
	delete(w.watched, path)
	w.Unlock()
	return nil
}

func (w *MockWatcher) Close() error {
	return nil
}

func (w *MockWatcher) Watched() map[string]struct{} {
	w.Lock()
	result := map[string]struct{}{}
	for k, v := range w.watched {
		result[k] = v
	}
	w.Unlock()
	return result
}
