package plugins

import (
	"bytes"
	"encoding/json"

	log "github.com/Sirupsen/logrus"
	"github.com/ugorji/go/codec"

	"github.com/weaveworks/scope/report"
)

func Reporter(pluginRegistry *Registry) probe.Reporter {
	return probe.ReporterFunc("plugins", func() (report.Report, error) {
		rpt := report.MakeReport()
		pluginRegistry.Implementors("reporter", func(plugin *Plugin) {
			var pluginReportJSON json.RawMessage
			if err := plugin.Call("Report", nil, &pluginReportJSON); err != nil {
				log.Errorf("plugins: error getting report from %s: %v", plugin.ID, err)
			}

			pluginReport := report.MakeReport()
			pluginReport.Plugins = pluginReport.Plugins.Add(plugin.PluginSpec)
			if err := codec.NewDecoder(bytes.NewReader(pluginReportJSON), &codec.JsonHandle{}).Decode(&pluginReport); err != nil {
				log.Errorf("plugins: error decoding report from %s: %v", plugin.ID, err)
			}
			rpt = rpt.Merge(pluginReport)
		})
		return rpt, nil
	})
}
