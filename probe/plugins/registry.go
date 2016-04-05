package plugins

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/fsnotify/fsnotify"
	"github.com/ugorji/go/codec"
	"golang.org/x/net/context"
	"golang.org/x/net/context/ctxhttp"

	"github.com/weaveworks/scope/common/backoff"
	"github.com/weaveworks/scope/common/fs"
	"github.com/weaveworks/scope/common/fswatch"
	"github.com/weaveworks/scope/common/xfer"
	"github.com/weaveworks/scope/report"
)

// Exposed for testing
var (
	transport = makeUnixRoundTripper
)

const (
	pluginTimeout = 500 * time.Millisecond
	pluginRetry   = 5 * time.Second
)

// Registry maintains a list of available plugins by name.
type Registry struct {
	rootPath          string
	apiVersion        string
	handshakeMetadata map[string]string
	pluginsBySocket   map[string]*Plugin
	lock              sync.RWMutex
	watcher           fswatch.Watcher
	done              chan struct{}
}

// NewRegistry creates a new registry which watches the given dir root for new
// plugins, and adds them.
func NewRegistry(rootPath, apiVersion string, handshakeMetadata map[string]string) (*Registry, error) {
	watcher, err := fswatch.NewWatcher()
	if err != nil {
		return nil, err
	}
	r := &Registry{
		rootPath:          rootPath,
		apiVersion:        apiVersion,
		handshakeMetadata: handshakeMetadata,
		pluginsBySocket:   map[string]*Plugin{},
		watcher:           watcher,
		done:              make(chan struct{}),
	}
	if err := r.addPath(context.Background(), r.rootPath); err != nil {
		r.Close()
		return nil, err
	}
	go r.loop()
	return r, nil
}

// add recursively crawls the path provided, adding it to the watcher, and
// looking for any existing sockets, loading them as plugins.
func (r *Registry) addPath(ctx context.Context, path string) error {
	// TODO: use of fs.Stat (which is syscall.Stat) here makes this linux specific.
	var statT syscall.Stat_t
	if err := fs.Stat(path, &statT); err != nil {
		return err
	}
	switch statT.Mode & syscall.S_IFMT {
	case syscall.S_IFDIR:
		if err := r.watcher.Add(path); err != nil {
			return err
		}
		files, err := fs.ReadDir(path)
		if err != nil {
			log.Errorf("plugins: error loading path %s: %v", path, err)
			return nil
		}
		for _, file := range files {
			r.addPath(ctx, filepath.Join(path, file.Name()))
		}
	case syscall.S_IFSOCK:
		if err := r.watcher.Add(path); err != nil {
			return err
		}
		if plugin, ok := r.pluginsBySocket[path]; ok {
			log.Infof("plugins: plugin already exists %s: conflicting %s", plugin.ID, path)
			return nil
		}
		tr, err := transport(path, pluginTimeout)
		if err != nil {
			log.Errorf("plugins: error loading plugin %s: %v", path, err)
			return nil
		}
		client := &http.Client{Transport: tr, Timeout: pluginTimeout}
		r.pluginsBySocket[path] = NewPlugin(ctx, path, client, r.apiVersion, r.handshakeMetadata)
	default:
		log.Infof("plugins: unknown filemode %s", path)
	}
	return nil
}

func (r *Registry) removePath(ctx context.Context, path string) error {
	plugin, ok := r.pluginsBySocket[path]
	if !ok {
		return nil
	}
	delete(r.pluginsBySocket, path)
	log.Errorf("plugins: removed plugin %s", plugin.socket)
	return plugin.Close()
}

func (r *Registry) loop() {
	for {
		select {
		case <-r.done:
			return
		case evt := <-r.watcher.Events():
			log.Errorf("plugins: event %v", evt)
			var handler func(context.Context, string) error
			switch evt.Op {
			case fsnotify.Create, fsnotify.Chmod:
				handler = r.addPath
			case fsnotify.Remove, fsnotify.Rename:
				handler = r.removePath
			case fsnotify.Write:
				continue
			default:
				log.Errorf("plugins: event %v: no handler", evt)
				continue
			}

			r.lock.Lock()
			if err := handler(context.Background(), evt.Name); err != nil {
				log.Errorf("plugins: event %v: error: %v", evt, err)
			}
			r.lock.Unlock()
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
	xfer.PluginSpec
	context context.Context
	socket  string
	client  *http.Client
	cancel  context.CancelFunc
	backoff backoff.Interface
}

// NewPlugin loads and initializes a new plugin. If client is nil,
// http.DefaultClient will be used.
func NewPlugin(ctx context.Context, socket string, client *http.Client, expectedAPIVersion string, handshakeMetadata map[string]string) *Plugin {
	params := url.Values{}
	for k, v := range handshakeMetadata {
		params.Add(k, v)
	}

	ctx, cancel := context.WithCancel(ctx)
	p := &Plugin{context: ctx, socket: socket, client: client, cancel: cancel}
	p.backoff = backoff.New(p.handshake(ctx, expectedAPIVersion, params), "plugin handshake")
	go p.backoff.Start()
	return p
}

type handshakeResponse struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Interfaces  []string `json:"interfaces"`
	APIVersion  string   `json:"api_version,omitempty"`
}

// handshake tries the handshake with this plugin.
func (p *Plugin) handshake(ctx context.Context, expectedAPIVersion string, params url.Values) func() (bool, error) {
	return func() (bool, error) {
		var resp handshakeResponse
		if err := p.get("/", params, &resp); err != nil {
			return false, fmt.Errorf("plugins: error loading plugin %s: %v", p.socket, err)
		}

		if resp.Name == "" {
			return false, fmt.Errorf("plugins: error loading plugin %s: plugin did not provide a name", p.socket)
		}
		if resp.APIVersion != expectedAPIVersion {
			return false, fmt.Errorf("plugins: error loading plugin %s: plugin did not provide correct API version: expected %q, got %q", p.socket, expectedAPIVersion, resp.APIVersion)
		}
		p.ID, p.Label = resp.Name, resp.Name
		p.Description = resp.Description
		p.Interfaces = resp.Interfaces
		log.Infof("plugins: loaded plugin %s: %s", p.ID, strings.Join(p.Interfaces, ", "))
		return true, nil
	}
}

// Report gets the latest report from the plugin
func (p *Plugin) Report() (report.Report, error) {
	result := report.MakeReport()
	err := p.get("/report", nil, &result)
	return result, err
}

// TODO(paulbellamy): better error handling on wrong status codes
func (p *Plugin) get(path string, params url.Values, result interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), pluginTimeout)
	defer cancel()
	resp, err := ctxhttp.Get(ctx, p.client, fmt.Sprintf("unix://%s?%s", path, params.Encode()))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return codec.NewDecoder(resp.Body, &codec.JsonHandle{}).Decode(&result)
}

// Close closes the client
func (p *Plugin) Close() error {
	p.backoff.Stop()
	p.cancel()
	return nil
}
