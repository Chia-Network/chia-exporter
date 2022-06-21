package metrics

import (
	"encoding/json"
	"fmt"

	"github.com/chia-network/go-chia-libs/pkg/rpc"
	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"

	wrappedPrometheus "github.com/chia-network/chia-exporter/internal/prometheus"
	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Harvester RPC calls are in this file

// HarvesterServiceMetrics contains all metrics related to the harvester
type HarvesterServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// Keep a local copy of the plot count, so we can do other actions when the value changes
	totalPlotsValue uint64

	// Farming Info Metrics
	totalPlots         *wrappedPrometheus.LazyGauge
	plotFilesize       *prometheus.GaugeVec
	totalFoundProofs   *wrappedPrometheus.LazyCounter
	lastFoundProofs    *wrappedPrometheus.LazyGauge
	totalEligiblePlots *wrappedPrometheus.LazyCounter
	lastEligiblePlots  *wrappedPrometheus.LazyGauge
	lastLookupTime     *wrappedPrometheus.LazyGauge
}

// InitMetrics sets all the metrics properties
func (s *HarvesterServiceMetrics) InitMetrics() {
	s.totalPlots = s.metrics.newGauge(chiaServiceHarvester, "total_plots", "Total number plots on this harvester")
	s.plotFilesize = s.metrics.newGaugeVec(chiaServiceHarvester, "plot_filesize", "Total filesize of plots on this harvester, by K size", []string{"size"})

	s.totalFoundProofs = s.metrics.newCounter(chiaServiceHarvester, "total_found_proofs", "Counter of total found proofs since the exporter started")
	s.lastFoundProofs = s.metrics.newGauge(chiaServiceHarvester, "last_found_proofs", "Number of proofs found for the last farmer_info event")

	s.totalEligiblePlots = s.metrics.newCounter(chiaServiceHarvester, "total_eligible_plots", "Counter of total eligible plots since the exporter started")
	s.lastEligiblePlots = s.metrics.newGauge(chiaServiceHarvester, "last_eligible_plots", "Number of eligible plots for the last farmer_info event")

	s.lastLookupTime = s.metrics.newGauge(chiaServiceHarvester, "last_lookup_time", "Lookup time for the last farmer_info event")
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with current/initial data
func (s *HarvesterServiceMetrics) InitialData() {
	utils.LogErr(s.metrics.client.HarvesterService.GetPlots())
}

// Disconnected clears/unregisters metrics when the connection drops
func (s *HarvesterServiceMetrics) Disconnected() {
	s.totalPlots.Unregister()
	s.lastFoundProofs.Unregister()
	s.lastEligiblePlots.Unregister()
	s.lastLookupTime.Unregister()
}

// ReceiveResponse handles crawler responses that are returned over the websocket
func (s *HarvesterServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	switch resp.Command {
	case "farming_info":
		s.FarmingInfo(resp)
	case "get_plots":
		s.GetPlots(resp)
	}
}

// FarmingInfo handles the farming_info event from the harvester
func (s *HarvesterServiceMetrics) FarmingInfo(resp *types.WebsocketResponse) {
	info := &types.EventHarvesterFarmingInfo{}
	err := json.Unmarshal(resp.Data, info)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	s.totalPlots.Set(float64(info.TotalPlots))
	if info.TotalPlots != s.totalPlotsValue {
		// Gets plot info (filesize, etc) when the number of plots changes
		utils.LogErr(s.metrics.client.HarvesterService.GetPlots())
	}
	s.totalPlotsValue = info.TotalPlots

	s.totalFoundProofs.Add(float64(info.FoundProofs))
	s.lastFoundProofs.Set(float64(info.FoundProofs))

	s.totalEligiblePlots.Add(float64(info.EligiblePlots))
	s.lastEligiblePlots.Set(float64(info.EligiblePlots))

	s.lastLookupTime.Set(info.Time)
}

// GetPlots handles a get_plots rpc response
func (s *HarvesterServiceMetrics) GetPlots(resp *types.WebsocketResponse) {
	plots := &rpc.HarvesterGetPlotsResponse{}
	err := json.Unmarshal(resp.Data, plots)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	// First, iterate through all the plots to get totals for each ksize
	plotSize := map[uint8]uint64{}
	for _, plot := range plots.Plots {
		kSize := plot.Size

		if _, ok := plotSize[kSize]; !ok {
			plotSize[kSize] = 0
		}

		plotSize[kSize] += plot.FileSize
	}

	// Now we can set the gauges with the calculated total values
	for kSize, fileSize := range plotSize {
		s.plotFilesize.WithLabelValues(fmt.Sprintf("%d", kSize)).Set(float64(fileSize))
	}
}
