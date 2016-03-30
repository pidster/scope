package plugins

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/paypal/ionet"

	fs_hook "github.com/weaveworks/scope/common/fs"
	fswatch_hook "github.com/weaveworks/scope/common/fswatch"
	"github.com/weaveworks/scope/test/fs"
	"github.com/weaveworks/scope/test/fswatch"
)

func stubTransport(fn func(socket string, timeout time.Duration) (http.RoundTripper, error)) {
	transport = fn
}
func restoreTransport() { transport = makeUnixRoundTripper }

type readWriteCloseRoundTripper struct {
	io.ReadWriteCloser
}

func (rwc readWriteCloseRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	conn := &closeableConn{
		Conn:   &ionet.Conn{R: rwc, W: rwc},
		Closer: rwc,
	}
	client := httputil.NewClientConn(conn, nil)
	defer client.Close()
	return client.Do(req)
}

// closeableConn gives us an overrideable Close, where ionet.Conn does not.
type closeableConn struct {
	net.Conn
	io.Closer
}

func (c *closeableConn) Close() error {
	c.Conn.Close()
	return c.Closer.Close()
}

type mockPlugin struct {
	t        *testing.T
	Name     string
	Handler  http.Handler
	Requests chan *http.Request
}

func (p mockPlugin) dir() string {
	return "/plugins"
}

func (p mockPlugin) path() string {
	return filepath.Join(p.dir(), p.base())
}

func (p mockPlugin) base() string {
	return p.Name + ".sock"
}

func (p mockPlugin) file() fs.File {
	incomingR, incomingW := io.Pipe()
	outgoingR, outgoingW := io.Pipe()
	go func() {
		conn := httputil.NewServerConn(&ionet.Conn{R: incomingR, W: outgoingW}, nil)
		req, err := conn.Read()
		if err != nil {
			p.t.Fatal(err)
		}
		resp := httptest.NewRecorder()
		p.Handler.ServeHTTP(resp, req)
		fmt.Fprintf(outgoingW, "HTTP/1.1 200 OK\nContent-Length: %d\n\n%s", resp.Body.Len(), resp.Body.String())
		if p.Requests != nil {
			p.Requests <- req
		}
	}()
	return fs.File{
		FName:   p.base(),
		FWriter: incomingW,
		FReader: outgoingR,
		FStat:   syscall.Stat_t{Mode: syscall.S_IFSOCK},
	}
}

type chanWriter chan []byte

func (w chanWriter) Write(p []byte) (int, error) {
	w <- p
	return len(p), nil
}

func (w chanWriter) Close() error {
	close(w)
	return nil
}

// TODO(paulbellamy): Would be nice to tie the fswatcher and the mock fs
// together, so adding/deleteing/etc would "just work"
func setup(t *testing.T, mockPlugins ...mockPlugin) (fs.Entry, *fswatch.MockWatcher) {
	sockets := []fs.Entry{}
	for _, p := range mockPlugins {
		sockets = append(sockets, p.file())
	}
	mockFS := fs.Dir("", fs.Dir("plugins", sockets...))
	fs_hook.Mock(
		mockFS)

	stubTransport(func(socket string, timeout time.Duration) (http.RoundTripper, error) {
		f, err := mockFS.Open(socket)
		return readWriteCloseRoundTripper{f}, err
	})

	mockWatcher := fswatch.NewMockWatcher()
	fswatch_hook.Mock(mockWatcher)
	return mockFS, mockWatcher
}

func restore(t *testing.T) {
	fs_hook.Restore()
	fswatch_hook.Restore()
	restoreTransport()
}

type iterator func(func(*Plugin))

func checkLoadedPlugins(t *testing.T, forEach iterator, expectedIDs []string) {
	pluginIDs := []string{}
	plugins := map[string]*Plugin{}
	forEach(func(p *Plugin) {
		pluginIDs = append(pluginIDs, p.ID)
		plugins[p.ID] = p
	})
	if len(pluginIDs) != len(expectedIDs) {
		t.Fatalf("Expected plugins %q, got: %q", expectedIDs, pluginIDs)
	}
	for i, id := range pluginIDs {
		if id != expectedIDs[i] {
			t.Fatalf("Expected plugins %q, got: %q", expectedIDs, pluginIDs)
		}
	}
}

// stringHandler returns an http.Handler which just prints the given string
func stringHandler(j string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, j)
	})
}

func TestRegistryLoadsExistingPlugins(t *testing.T) {
	setup(t, mockPlugin{t: t, Name: "testPlugin", Handler: stringHandler(`{"name":"testPlugin","interfaces":["reporter"],"api_version":"1"}`)})
	defer restore(t)

	root := "/plugins"
	r, err := NewRegistry(root, "1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	checkLoadedPlugins(t, r.ForEach, []string{"testPlugin"})
}

func TestRegistryDiscoversNewPlugins(t *testing.T) {
	mockFS, mockWatcher := setup(t)
	defer restore(t)

	root := "/plugins"
	r, err := NewRegistry(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	checkLoadedPlugins(t, r.ForEach, []string{})

	// Add the new plugin
	plugin := mockPlugin{t: t, Name: "testPlugin", Requests: make(chan *http.Request), Handler: stringHandler(`{"name":"testPlugin","interfaces":["reporter"]}`)}
	mockFS.Add(plugin.dir(), plugin.file())
	mockWatcher.Events() <- fsnotify.Event{Name: plugin.path(), Op: fsnotify.Create}
	select {
	case <-plugin.Requests:
		// registry connected to this plugin
	case <-time.After(100 * time.Millisecond):
		// timeout
		t.Errorf("timeout waiting for registry to connect to new plugin")
	}

	checkLoadedPlugins(t, r.ForEach, []string{"testPlugin"})

	if _, ok := mockWatcher.Watched()[plugin.path()]; !ok {
		t.Errorf("Expected registry to be watching %s, but wasn't", plugin.path())
	}
}

func TestRegistryRemovesPlugins(t *testing.T) {
	plugin := mockPlugin{t: t, Name: "testPlugin", Requests: make(chan *http.Request), Handler: stringHandler(`{"name":"testPlugin","interfaces":["reporter"]}`)}
	_, mockWatcher := setup(t, plugin)
	defer restore(t)

	root := "/plugins"
	r, err := NewRegistry(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	checkLoadedPlugins(t, r.ForEach, []string{"testPlugin"})

	// Remove the plugin
	mockWatcher.Events() <- fsnotify.Event{Name: plugin.path(), Op: fsnotify.Remove}
	select {
	case <-plugin.Requests:
		// registry closed connection to this plugin
	case <-time.After(100 * time.Millisecond):
		// timeout
		t.Errorf("timeout waiting for registry to remove plugin")
	}

	checkLoadedPlugins(t, r.ForEach, []string{})

	if _, ok := mockWatcher.Watched()[plugin.path()]; ok {
		t.Errorf("Expected registry not to be watching %s, but was", plugin.path())
	}
}

func TestRegistryReturnsPluginsByInterface(t *testing.T) {
	setup(
		t,
		mockPlugin{
			t:       t,
			Name:    "plugin1",
			Handler: stringHandler(`{"name":"plugin1","interfaces":["reporter"]}`),
		},
		mockPlugin{
			t:       t,
			Name:    "plugin2",
			Handler: stringHandler(`{"name":"plugin2","interfaces":["other"]}`),
		},
	)
	defer restore(t)

	root := "/plugins"
	r, err := NewRegistry(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	checkLoadedPlugins(t, r.ForEach, []string{"plugin1", "plugin2"})
	checkLoadedPlugins(t, func(fn func(*Plugin)) { r.Implementors("reporter", fn) }, []string{"plugin1"})
	checkLoadedPlugins(t, func(fn func(*Plugin)) { r.Implementors("other", fn) }, []string{"plugin2"})
}

func TestRegistryHandlesConflictingPlugins(t *testing.T) {
	setup(
		t,
		mockPlugin{
			t:       t,
			Name:    "plugin1",
			Handler: stringHandler(`{"name":"plugin1","interfaces":["reporter"]}`),
		},
		mockPlugin{
			t:       t,
			Name:    "plugin1",
			Handler: stringHandler(`{"name":"plugin2","interfaces":["other"]}`),
		},
	)
	defer restore(t)

	root := "/plugins"
	r, err := NewRegistry(root, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// Should just have the second one (we just log conflicts)
	checkLoadedPlugins(t, r.ForEach, []string{"plugin2"})
	checkLoadedPlugins(t, func(fn func(*Plugin)) { r.Implementors("other", fn) }, []string{"plugin2"})
}
