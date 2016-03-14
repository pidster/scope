// fswatch is a wrapper around github.com/fsnotify/fsnotify, which lets us mock
// out the watcher for testing.
package fswatch

import (
	"github.com/fsnotify/fsnotify"
)

// Watcher is the interface of a fsnotify watcher
type Watcher interface {
	Events() chan fsnotify.Event
	Errors() chan error
	Add(string) error
	Remove(string) error
	Close() error
}

var stub = realNewWatcher

func realNewWatcher() (Watcher, error) {
	w, err := fsnotify.NewWatcher()
	return wrapper{w}, err
}

type wrapper struct{ *fsnotify.Watcher }

func (w wrapper) Events() chan fsnotify.Event { return w.Watcher.Events }
func (w wrapper) Errors() chan error          { return w.Watcher.Errors }

// NewWatcher see fsnotify.NewWatcher
func NewWatcher() (Watcher, error) {
	return stub()
}

// Mock is used to switch out the filesystem for a mock.
func Mock(mock Watcher) {
	stub = func() (Watcher, error) {
		return mock, nil
	}
}

// Restore puts back the real filesystem.
func Restore() {
	stub = realNewWatcher
}
