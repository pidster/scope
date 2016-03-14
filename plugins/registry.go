package plugins

import (
	"fmt"
	"io"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/fsnotify/fsnotify"

	"github.com/weaveworks/scope/common/fs"
	"github.com/weaveworks/scope/common/fswatch"
)

var (
	// made available for testing
	dialer = net.DialTimeout

	ErrTimeout = fmt.Errorf("rpc call timeout")
)

const (
	pluginTimeout = 500 * time.Millisecond
)

// Registry maintains a list of available plugins by name.
type Registry struct {
	root              string
	apiVersion        string
	handshakeMetadata map[string]string
	pluginsBySocket   map[string]*Plugin
	lock              sync.RWMutex
	watcher           fswatch.Watcher
	done              chan struct{}
}

// NewRegistry creates a new registry which watches the given dir root for new
// plugins, and adds them.
func NewRegistry(root, apiVersion string, handshakeMetadata map[string]string) (*Registry, error) {
	watcher, err := fswatch.NewWatcher()
	if err != nil {
		return nil, err
	}
	r := &Registry{
		root:              root,
		apiVersion:        apiVersion,
		handshakeMetadata: handshakeMetadata,
		pluginsBySocket:   map[string]*Plugin{},
		watcher:           watcher,
		done:              make(chan struct{}),
	}
	if err := r.addPath(r.root); err != nil {
		r.Close()
		return nil, err
	}
	go r.loop()
	return r, nil
}

// add recursively crawls the path provided, adding it to the watcher, and
// looking for any existing sockets, loading them as plugins.
func (r *Registry) addPath(path string) error {
	var statT syscall.Stat_t
	if err := fs.Stat(path, &statT); err != nil {
		return err
	}
	if err := r.watcher.Add(path); err != nil {
		return err
	}
	// TODO: use of fs.Stat (which is syscall.Stat) here makes this linux specific.
	switch statT.Mode & syscall.S_IFMT {
	case syscall.S_IFDIR:
		files, err := fs.ReadDir(path)
		if err != nil {
			log.Errorf("plugins: error loading path %s: %v", path, err)
			return nil
		}
		for _, file := range files {
			if err := r.addPath(filepath.Join(path, file.Name())); err != nil {
				log.Errorf("plugins: error loading path %s: %v", filepath.Join(path, file.Name()), err)
				return nil
			}
		}
	case syscall.S_IFSOCK:
		if plugin, ok := r.pluginsBySocket[path]; ok {
			log.Infof("plugins: plugin already exists %s: conflicting %s", plugin.ID, path)
			return nil
		}

		conn, err := dialer("unix", path, 1*time.Second)
		if err != nil {
			log.Errorf("plugins: error loading plugin %s: %v", path, err)
			return nil
		}
		transport := &onClose{conn, func() error { r.removePath(path); return nil }}
		plugin, err := NewPlugin(transport, r.apiVersion, r.handshakeMetadata)
		if err != nil {
			log.Errorf("plugins: error loading plugin %s: %v", path, err)
			return nil
		}

		log.Infof("plugins: loaded plugin %s: %s", plugin.ID, strings.Join(plugin.Interfaces, ", "))
		r.pluginsBySocket[path] = plugin
	default:
		log.Infof("plugins: unknown filemode %s", path)
	}
	return nil
}

func (r *Registry) removePath(path string) error {
	r.watcher.Remove(path)
	if plugin, ok := r.pluginsBySocket[path]; ok {
		delete(r.pluginsBySocket, path)
		return plugin.Close()
	}
	return nil
}

func (r *Registry) loop() {
	for {
		select {
		case <-r.done:
			return
		case evt := <-r.watcher.Events():
			handlers := map[fsnotify.Op]func(string) error{
				fsnotify.Create: r.addPath,
				fsnotify.Remove: r.removePath,
				fsnotify.Chmod:  r.addPath,
			}
			if handler, ok := handlers[evt.Op]; ok {
				r.lock.Lock()
				if err := handler(evt.Name); err != nil {
					log.Errorf("plugins: event %v: error: %v", evt, err)
				}
				r.lock.Unlock()
			} else {
				log.Errorf("plugins: event %v: no handler", evt)
			}
		case err := <-r.watcher.Errors():
			log.Errorf("plugins: error: %v", err)
		}
	}
}

// ForEach walks through all the plugins running f for each one.
func (r *Registry) ForEach(f func(p *Plugin)) {
	r.lock.RLock()
	defer r.lock.RUnlock()
	paths := []string{}
	for path, _ := range r.pluginsBySocket {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		f(r.pluginsBySocket[path])
	}
}

// Implementors walks the available plugins fulfilling the given interface
func (r *Registry) Implementors(iface string, f func(p *Plugin)) {
	r.ForEach(func(p *Plugin) {
		for _, piface := range p.Interfaces {
			if piface == iface {
				f(p)
			}
		}
	})
}

// Close shuts down the registry. It can still be used after this, but will be
// out of date.
func (r *Registry) Close() error {
	close(r.done)
	r.lock.Lock()
	defer r.lock.Unlock()
	for _, plugin := range r.pluginsBySocket {
		plugin.Close()
	}
	return r.watcher.Close()
}

type Plugin struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description,omitempty"`
	Interfaces  []string `json:"interfaces"`
	conn        io.ReadWriteCloser
	client      *rpc.Client
}

type handshakeResponse struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Interfaces  []string `json:"interfaces"`
	APIVersion  string   `json:"api_version,omitempty"`
}

// NewPlugin loads and initializes a new plugin
func NewPlugin(conn io.ReadWriteCloser, expectedAPIVersion string, handshakeMetadata map[string]string) (*Plugin, error) {
	p := &Plugin{conn: conn, client: jsonrpc.NewClient(conn)}
	var resp handshakeResponse
	if err := p.Call("Handshake", handshakeMetadata, &resp); err != nil {
		return nil, err
	}
	if resp.Name == "" {
		return nil, fmt.Errorf("plugin did not provide a name")
	}
	if resp.APIVersion != expectedAPIVersion {
		return nil, fmt.Errorf("plugin did not provide correct API version: expected %q, got %q", expectedAPIVersion, resp.APIVersion)
	}
	p.ID, p.Label = resp.Name, resp.Name
	p.Description = resp.Description
	p.Interfaces = resp.Interfaces
	return p, nil
}

// Call calls some method on the remote plugin. Should replace this with a
// dynamic interface thing maybe.
func (p *Plugin) Call(cmd string, args interface{}, reply interface{}) error {
	err := make(chan error, 1)
	go func() {
		err <- p.client.Call("Plugin."+cmd, args, reply)
	}()
	select {
	case e := <-err:
		if e == rpc.ErrShutdown {
			p.Close()
		}
		return e
	case <-time.After(pluginTimeout):
		// timeout
	}
	return ErrTimeout
}

// Close closes the rpc client
func (p *Plugin) Close() error {
	return p.client.Close()
}

// onClose lets us attach a callback to be called after the underlying
// transport Closes
type onClose struct {
	io.ReadWriteCloser
	fn func() error
}

func (c *onClose) Close() error {
	err := c.ReadWriteCloser.Close()
	if c.fn != nil {
		err2 := c.fn()
		if err == nil {
			err = err2
		}
	}
	return err
}
