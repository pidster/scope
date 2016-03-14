package detailed

import (
	"github.com/weaveworks/scope/report"
)

func NodeMetadata(r report.Report, n report.Node) []report.MetadataRow {
	if _, ok := n.Counters.Lookup(n.Topology); ok {
		// This is a group of nodes, so no metadata!
		return nil
	}

	if topology, ok := r.Topology(n.Topology); ok {
		return topology.MetadataTemplates.MetadataRows(n)
	}
	return nil
}
