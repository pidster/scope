package plugins

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"sort"

	"github.com/davecgh/go-spew/spew"
	"github.com/mndrix/ps"
	"github.com/ugorji/go/codec"

	"github.com/weaveworks/scope/test/reflect"
)

// PluginSet is a set of plugins keyed on ID. Clients must use
// the Add method to add plugins
type PluginSet struct {
	psMap ps.Map
}

// EmptyPluginSet is the empty set of plugins.
var EmptyPluginSet = PluginSet{ps.NewMap()}

// MakePluginSet makes a new PluginSet with the given plugins.
func MakePluginSet(plugins ...*Plugin) PluginSet {
	return EmptyPluginSet.Add(plugins...)
}

// Add adds the plugins to the PluginSet. Add is the only valid way to grow a
// PluginSet. Add returns the PluginSet to enable chaining.
func (n PluginSet) Add(plugins ...*Plugin) PluginSet {
	result := n.psMap
	if result == nil {
		result = ps.NewMap()
	}
	for _, plugin := range plugins {
		result = result.Set(plugin.ID, plugin)
	}
	return PluginSet{result}
}

// Merge combines the two PluginSets and returns a new result.
func (n PluginSet) Merge(other PluginSet) PluginSet {
	nSize, otherSize := n.Size(), other.Size()
	if nSize == 0 {
		return other
	}
	if otherSize == 0 {
		return n
	}
	result, iter := n.psMap, other.psMap
	if nSize < otherSize {
		result, iter = iter, result
	}
	iter.ForEach(func(key string, otherVal interface{}) {
		result = result.Set(key, otherVal)
	})
	return PluginSet{result}
}

// Lookup the plugin 'key'
func (n PluginSet) Lookup(key string) (*Plugin, bool) {
	if n.psMap != nil {
		value, ok := n.psMap.Lookup(key)
		if ok {
			return value.(*Plugin), true
		}
	}
	return nil, false
}

// Keys is a list of all the keys in this set.
func (n PluginSet) Keys() []string {
	if n.psMap == nil {
		return nil
	}
	k := n.psMap.Keys()
	sort.Strings(k)
	return k
}

// Size is the number of plugins in the set
func (n PluginSet) Size() int {
	if n.psMap == nil {
		return 0
	}
	return n.psMap.Size()
}

// ForEach executes f for each plugin in the set. Nodes are traversed in sorted
// order.
func (n PluginSet) ForEach(f func(*Plugin)) {
	for _, key := range n.Keys() {
		if val, ok := n.psMap.Lookup(key); ok {
			f(val.(*Plugin))
		}
	}
}

// Copy is a noop
func (n PluginSet) Copy() PluginSet {
	return n
}

func (n PluginSet) String() string {
	keys := []string{}
	if n.psMap == nil {
		n = EmptyPluginSet
	}
	psMap := n.psMap
	if psMap == nil {
		psMap = ps.NewMap()
	}
	for _, k := range psMap.Keys() {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := bytes.NewBufferString("{")
	for _, key := range keys {
		val, _ := psMap.Lookup(key)
		fmt.Fprintf(buf, "%s: %s, ", key, spew.Sdump(val))
	}
	fmt.Fprintf(buf, "}")
	return buf.String()
}

// DeepEqual tests equality with other PluginSets
func (n PluginSet) DeepEqual(i interface{}) bool {
	d, ok := i.(PluginSet)
	if !ok {
		return false
	}

	if n.Size() != d.Size() {
		return false
	}
	if n.Size() == 0 {
		return true
	}

	equal := true
	n.psMap.ForEach(func(k string, val interface{}) {
		if otherValue, ok := d.psMap.Lookup(k); !ok {
			equal = false
		} else {
			equal = equal && reflect.DeepEqual(val, otherValue)
		}
	})
	return equal
}

func (n PluginSet) toIntermediate() []*Plugin {
	intermediate := make([]*Plugin, 0, n.Size())
	n.ForEach(func(plugin *Plugin) {
		intermediate = append(intermediate, plugin)
	})
	return intermediate
}

func (n PluginSet) fromIntermediate(plugins []*Plugin) PluginSet {
	return MakePluginSet(plugins...)
}

// CodecEncodeSelf implements codec.Selfer
func (n *PluginSet) CodecEncodeSelf(encoder *codec.Encoder) {
	if n.psMap != nil {
		encoder.Encode(n.toIntermediate())
	} else {
		encoder.Encode(nil)
	}
}

// CodecDecodeSelf implements codec.Selfer
func (n *PluginSet) CodecDecodeSelf(decoder *codec.Decoder) {
	in := []*Plugin{}
	if err := decoder.Decode(&in); err != nil {
		return
	}
	*n = PluginSet{}.fromIntermediate(in)
}

// MarshalJSON shouldn't be used, use CodecEncodeSelf instead
func (PluginSet) MarshalJSON() ([]byte, error) {
	panic("MarshalJSON shouldn't be used, use CodecEncodeSelf instead")
}

// UnmarshalJSON shouldn't be used, use CodecDecodeSelf instead
func (*PluginSet) UnmarshalJSON(b []byte) error {
	panic("UnmarshalJSON shouldn't be used, use CodecDecodeSelf instead")
}

// GobEncode implements gob.Marshaller
func (n PluginSet) GobEncode() ([]byte, error) {
	buf := bytes.Buffer{}
	err := gob.NewEncoder(&buf).Encode(n.toIntermediate())
	return buf.Bytes(), err
}

// GobDecode implements gob.Unmarshaller
func (n *PluginSet) GobDecode(input []byte) error {
	in := []*Plugin{}
	if err := gob.NewDecoder(bytes.NewBuffer(input)).Decode(&in); err != nil {
		return err
	}
	*n = PluginSet{}.fromIntermediate(in)
	return nil
}
