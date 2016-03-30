package plugins

import (
	"bytes"
	"io"
	"io/ioutil"
	"net"
	"path/filepath"
	"strings"
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

func stubDialer(fn func(network, address string, timeout time.Duration) (net.Conn, error)) {
	dialer = fn
}
func restoreDialer() { dialer = net.DialTimeout }

type NopWriteCloser struct{ io.Writer }

func (n NopWriteCloser) Close() error { return nil }

type mockPlugin struct {
	Name   string
	R      io.Reader
	W      io.Writer
	Closer io.Closer
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
	return fs.File{
		FName:   p.base(),
		FReader: p.R,
		FWriter: p.W,
		FCloser: p.Closer,
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

	stubDialer(func(network, address string, timeout time.Duration) (net.Conn, error) {
		if network != "unix" {
			t.Fatalf("Expected dial to unix socket, got: %q", network)
		}
		f, err := mockFS.Open(address)
		return &closeableConn{&ionet.Conn{R: f, W: f}, f}, err
	})

	mockWatcher := fswatch.NewMockWatcher()
	fswatch_hook.Mock(mockWatcher)
	return mockFS, mockWatcher
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

func restore(t *testing.T) {
	fs_hook.Restore()
	fswatch_hook.Restore()
	restoreDialer()
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

func TestRegistryLoadsExistingPlugins(t *testing.T) {
	rBuf := bytes.NewBufferString(`{"id":0,"result":{"name":"testPlugin","interfaces":["reporter"],"api_version":"1"}}`)
	setup(t, mockPlugin{Name: "testPlugin", R: rBuf, W: ioutil.Discard})
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
	rBuf := bytes.NewBufferString(`{"id":0,"result":{"name":"testPlugin","interfaces":["reporter"]}}`)
	w := chanWriter(make(chan []byte))
	plugin := mockPlugin{Name: "testPlugin", R: rBuf, W: w}
	mockFS.Add(plugin.dir(), plugin.file())
	mockWatcher.Events() <- fsnotify.Event{Name: plugin.path(), Op: fsnotify.Create}
	select {
	case <-w:
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
	rBuf := bytes.NewBufferString(`{"id":0,"result":{"name":"testPlugin","interfaces":["reporter"]}}`)
	plugin := mockPlugin{Name: "testPlugin", R: rBuf, Closer: chanWriter(make(chan []byte))}
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
	case <-plugin.Closer.(chanWriter):
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

func TestRegistryRemovesPluginsWhenTheyClose(t *testing.T) {
	// the reader here will EOF after this message, which should count as the
	// connection closing.
	rBuf := strings.NewReader(`{"id":0,"result":{"name":"testPlugin","interfaces":["reporter"]}}`)
	plugin := mockPlugin{Name: "testPlugin", R: rBuf, Closer: chanWriter(make(chan []byte))}
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
	select {
	case <-plugin.Closer.(chanWriter):
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
			Name: "plugin1",
			R:    bytes.NewBufferString(`{"id":0,"result":{"name":"plugin1","interfaces":["reporter"]}}`),
			W:    ioutil.Discard,
		},
		mockPlugin{
			Name: "plugin2",
			R:    bytes.NewBufferString(`{"id":0,"result":{"name":"plugin2","interfaces":["other"]}}`),
			W:    ioutil.Discard,
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
			Name: "plugin1",
			R:    bytes.NewBufferString(`{"id":0,"result":{"name":"plugin1","interfaces":["reporter"]}}`),
			W:    ioutil.Discard,
		},
		mockPlugin{
			Name: "plugin1",
			R:    bytes.NewBufferString(`{"id":0,"result":{"name":"plugin2","interfaces":["other"]}}`),
			W:    ioutil.Discard,
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
