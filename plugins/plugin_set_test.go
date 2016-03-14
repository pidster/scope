package plugins

import (
	"fmt"
	"testing"

	"github.com/weaveworks/scope/test/reflect"
)

var benchmarkResult PluginSet

func TestMakePluginSet(t *testing.T) {
	for _, testcase := range []struct {
		inputs []string
		wants  []string
	}{
		{inputs: nil, wants: nil},
		{
			inputs: []string{"a"},
			wants:  []string{"a"},
		},
		{
			inputs: []string{"a", "a"},
			wants:  []string{"a"},
		},
		{
			inputs: []string{"b", "c", "a"},
			wants:  []string{"a", "b", "c"},
		},
	} {
		var inputs []*Plugin
		for _, id := range testcase.inputs {
			inputs = append(inputs, &Plugin{ID: id})
		}
		have := MakePluginSet(inputs...)
		var haveIDs []string
		have.ForEach(func(p *Plugin) {
			haveIDs = append(haveIDs, p.ID)
		})
		if !reflect.DeepEqual(testcase.wants, haveIDs) {
			t.Errorf("%#v: want %#v, have %#v", inputs, testcase.wants, haveIDs)
		}
	}
}

func BenchmarkMakePluginSet(b *testing.B) {
	plugins := []*Plugin{}
	for i := 1000; i >= 0; i-- {
		plugins = append(plugins, &Plugin{ID: fmt.Sprint(i)})
	}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkResult = MakePluginSet(plugins...)
	}
}

func TestPluginSetAdd(t *testing.T) {
	for _, testcase := range []struct {
		input   PluginSet
		plugins []*Plugin
		want    PluginSet
	}{
		{
			input:   PluginSet{},
			plugins: []*Plugin{},
			want:    PluginSet{},
		},
		{
			input:   EmptyPluginSet,
			plugins: []*Plugin{},
			want:    EmptyPluginSet,
		},
		{
			input:   MakePluginSet(&Plugin{ID: "a"}),
			plugins: []*Plugin{},
			want:    MakePluginSet(&Plugin{ID: "a"}),
		},
		{
			input:   EmptyPluginSet,
			plugins: []*Plugin{&Plugin{ID: "a"}},
			want:    MakePluginSet(&Plugin{ID: "a"}),
		},
		{
			input:   MakePluginSet(&Plugin{ID: "a"}),
			plugins: []*Plugin{&Plugin{ID: "a"}},
			want:    MakePluginSet(&Plugin{ID: "a"}),
		},
		{
			input: MakePluginSet(&Plugin{ID: "b"}),
			plugins: []*Plugin{
				&Plugin{ID: "a"},
				&Plugin{ID: "b"},
			},
			want: MakePluginSet(
				&Plugin{ID: "a"},
				&Plugin{ID: "b"},
			),
		},
		{
			input: MakePluginSet(&Plugin{ID: "a"}),
			plugins: []*Plugin{
				&Plugin{ID: "c"},
				&Plugin{ID: "b"},
			},
			want: MakePluginSet(
				&Plugin{ID: "a"},
				&Plugin{ID: "b"},
				&Plugin{ID: "c"},
			),
		},
		{
			input: MakePluginSet(
				&Plugin{ID: "a"},
				&Plugin{ID: "c"},
			),
			plugins: []*Plugin{
				&Plugin{ID: "b"},
				&Plugin{ID: "b"},
				&Plugin{ID: "b"},
			},
			want: MakePluginSet(
				&Plugin{ID: "a"},
				&Plugin{ID: "b"},
				&Plugin{ID: "c"},
			),
		},
	} {
		originalLen := testcase.input.Size()
		if want, have := testcase.want, testcase.input.Add(testcase.plugins...); !reflect.DeepEqual(want, have) {
			t.Errorf("%v + %v: want %v, have %v", testcase.input, testcase.plugins, want, have)
		}
		if testcase.input.Size() != originalLen {
			t.Errorf("%v + %v: modified the original input!", testcase.input, testcase.plugins)
		}
	}
}

func BenchmarkPluginSetAdd(b *testing.B) {
	n := EmptyPluginSet
	for i := 0; i < 600; i++ {
		n = n.Add(&Plugin{ID: fmt.Sprint(i)})
	}

	plugin := &Plugin{ID: "401.5"}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkResult = n.Add(plugin)
	}
}

func TestPluginSetMerge(t *testing.T) {
	for _, testcase := range []struct {
		input PluginSet
		other PluginSet
		want  PluginSet
	}{
		{input: PluginSet{}, other: PluginSet{}, want: PluginSet{}},
		{input: EmptyPluginSet, other: EmptyPluginSet, want: EmptyPluginSet},
		{
			input: MakePluginSet(&Plugin{ID: "a"}),
			other: EmptyPluginSet,
			want:  MakePluginSet(&Plugin{ID: "a"}),
		},
		{
			input: EmptyPluginSet,
			other: MakePluginSet(&Plugin{ID: "a"}),
			want:  MakePluginSet(&Plugin{ID: "a"}),
		},
		{
			input: MakePluginSet(&Plugin{ID: "a"}),
			other: MakePluginSet(&Plugin{ID: "b"}),
			want:  MakePluginSet(&Plugin{ID: "a"}, &Plugin{ID: "b"}),
		},
		{
			input: MakePluginSet(&Plugin{ID: "b"}),
			other: MakePluginSet(&Plugin{ID: "a"}),
			want:  MakePluginSet(&Plugin{ID: "a"}, &Plugin{ID: "b"}),
		},
		{
			input: MakePluginSet(&Plugin{ID: "a"}),
			other: MakePluginSet(&Plugin{ID: "a"}),
			want:  MakePluginSet(&Plugin{ID: "a"}),
		},
		{
			input: MakePluginSet(&Plugin{ID: "a"}, &Plugin{ID: "c"}),
			other: MakePluginSet(&Plugin{ID: "a"}, &Plugin{ID: "b"}),
			want:  MakePluginSet(&Plugin{ID: "a"}, &Plugin{ID: "b"}, &Plugin{ID: "c"}),
		},
		{
			input: MakePluginSet(&Plugin{ID: "b"}),
			other: MakePluginSet(&Plugin{ID: "a"}),
			want:  MakePluginSet(&Plugin{ID: "a"}, &Plugin{ID: "b"}),
		},
	} {
		originalLen := testcase.input.Size()
		if want, have := testcase.want, testcase.input.Merge(testcase.other); !reflect.DeepEqual(want, have) {
			t.Errorf("%v + %v: want %v, have %v", testcase.input, testcase.other, want, have)
		}
		if testcase.input.Size() != originalLen {
			t.Errorf("%v + %v: modified the original input!", testcase.input, testcase.other)
		}
	}
}

func BenchmarkPluginSetMerge(b *testing.B) {
	n, other := PluginSet{}, PluginSet{}
	for i := 0; i < 600; i++ {
		n = n.Add(&Plugin{ID: fmt.Sprint(i)})
	}

	for i := 400; i < 1000; i++ {
		other = other.Add(&Plugin{ID: fmt.Sprint(i)})
	}
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchmarkResult = n.Merge(other)
	}
}
