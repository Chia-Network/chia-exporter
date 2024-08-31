package metrics

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/chia-network/go-chia-libs/pkg/protocols"
	"github.com/chia-network/go-chia-libs/pkg/rpc"
	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"

	wrappedPrometheus "github.com/chia-network/go-modules/pkg/prometheus"

	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Harvester RPC calls are in this file

// HarvesterServiceMetrics contains all metrics related to the harvester
type HarvesterServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// General Service Metrics
	version *prometheus.GaugeVec

	// Connection Metrics
	connectionCount *prometheus.GaugeVec

	// Keep a local copy of the plot count, so we can do other actions when the value changes
	totalPlotsValue uint64

	// Farming Info Metrics
	totalPlots         *wrappedPrometheus.LazyGauge
	plotFilesize       *prometheus.GaugeVec
	plotCount          *prometheus.GaugeVec
	totalFoundProofs   *wrappedPrometheus.LazyCounter
	lastFoundProofs    *wrappedPrometheus.LazyGauge
	totalEligiblePlots *wrappedPrometheus.LazyCounter
	lastEligiblePlots  *wrappedPrometheus.LazyGauge
	lastLookupTime     *wrappedPrometheus.LazyGauge

	// Debug Metric
	debug *prometheus.GaugeVec

	// Tracking certain requests to make sure only one happens at any given time
	gettingPlots bool
}

// InitMetrics sets all the metrics properties
func (s *HarvesterServiceMetrics) InitMetrics(network *string) {
	// General Service Metrics
	s.version = s.metrics.newGaugeVec(chiaServiceHarvester, "version", "The version of chia-blockchain the service is running", []string{"version"})

	// Connection Metrics
	s.connectionCount = s.metrics.newGaugeVec(chiaServiceHarvester, "connection_count", "Number of active connections for each type of peer", []string{"node_type"})

	plotLabels := []string{"size", "type", "compression"}
	s.totalPlots = s.metrics.newGauge(chiaServiceHarvester, "total_plots", "Total number of plots on this harvester")
	s.plotFilesize = s.metrics.newGaugeVec(chiaServiceHarvester, "plot_filesize", "Total filesize of plots on this harvester, by K size", plotLabels)
	s.plotCount = s.metrics.newGaugeVec(chiaServiceHarvester, "plot_count", "Total count of plots on this harvester, by K size", plotLabels)

	s.totalFoundProofs = s.metrics.newCounter(chiaServiceHarvester, "total_found_proofs", "Counter of total found proofs since the exporter started")
	s.lastFoundProofs = s.metrics.newGauge(chiaServiceHarvester, "last_found_proofs", "Number of proofs found for the last farmer_info event")

	s.totalEligiblePlots = s.metrics.newCounter(chiaServiceHarvester, "total_eligible_plots", "Counter of total eligible plots since the exporter started")
	s.lastEligiblePlots = s.metrics.newGauge(chiaServiceHarvester, "last_eligible_plots", "Number of eligible plots for the last farmer_info event")

	s.lastLookupTime = s.metrics.newGauge(chiaServiceHarvester, "last_lookup_time", "Lookup time for the last farmer_info event")

	// Debug Metric
	s.debug = s.metrics.newGaugeVec(chiaServiceHarvester, "debug_metrics", "random debugging metrics distinguished by labels", []string{"key"})
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with current/initial data
func (s *HarvesterServiceMetrics) InitialData() {
	// Only get the version on an initial or reconnection
	utils.LogErr(s.metrics.client.HarvesterService.GetVersion(&rpc.GetVersionOptions{}))

	s.httpGetPlots()
}

// SetupPollingMetrics starts any metrics that happen on an interval
func (s *HarvesterServiceMetrics) SetupPollingMetrics() {
	go func() {
		for {
			utils.LogErr(s.metrics.client.HarvesterService.GetConnections(&rpc.GetConnectionsOptions{}))
			time.Sleep(15 * time.Second)
		}
	}()
}

func (s *HarvesterServiceMetrics) httpGetPlots() {
	if s.gettingPlots {
		log.Debug("Skipping get_plots since another request is already in flight")
		return
	}
	s.gettingPlots = true
	defer func() {
		s.gettingPlots = false
	}()

	// get_plots seems to sometimes not respond on websockets, so doing http request for this
	log.Debug("Calling get_plots with http client")
	plots, _, err := s.metrics.httpClient.HarvesterService.GetPlots()
	if err != nil {
		log.Warnf("Could not get plot information from harvester: %s\n", err.Error())
		return
	}

	s.ProcessGetPlots(plots)
}

// Disconnected clears/unregisters metrics when the connection drops
func (s *HarvesterServiceMetrics) Disconnected() {
	s.version.Reset()
	s.connectionCount.Reset()
	s.totalPlots.Unregister()
	s.plotFilesize.Reset()
	s.plotCount.Reset()
	s.lastFoundProofs.Unregister()
	s.lastEligiblePlots.Unregister()
	s.lastLookupTime.Unregister()
}

// Reconnected is called when the service is reconnected after the websocket was disconnected
func (s *HarvesterServiceMetrics) Reconnected() {
	s.InitialData()
}

// ReceiveResponse handles crawler responses that are returned over the websocket
func (s *HarvesterServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	switch resp.Command {
	case "get_version":
		versionHelper(resp, s.version)
	case "get_connections":
		s.GetConnections(resp)
	case "farming_info":
		s.FarmingInfo(resp)
	case "get_plots":
		s.GetPlots(resp)
	case "debug":
		debugHelper(resp, s.debug)
	}
}

// GetConnections handler for get_connections events
func (s *HarvesterServiceMetrics) GetConnections(resp *types.WebsocketResponse) {
	connectionCountHelper(resp, s.connectionCount)
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
	log.Debugf("New Plot Count: %d | Previous Plot Count: %d\n", info.TotalPlots, s.totalPlotsValue)
	// We actually set the _new_ value of totalPlotsValue in the get_plots handler, to make sure that request was successful
	if info.TotalPlots != s.totalPlotsValue {
		// Gets plot info (filesize, etc) when the number of plots changes
		s.httpGetPlots()
	}

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

	s.ProcessGetPlots(plots)
}

// PlotType is the type of plot (og or pool)
type PlotType uint8

const (
	// PlotTypeOg is the original plot format, no plotNFT
	PlotTypeOg = PlotType(0)
	// PlotTypePool is the new plotNFT plot format
	PlotTypePool = PlotType(1)
)

// ProcessGetPlots processes the `GetPlotsResponse` from get_plots so that we can use this with websockets or HTTP RPC requests
func (s *HarvesterServiceMetrics) ProcessGetPlots(plots *rpc.HarvesterGetPlotsResponse) {
	plotSize, plotCount := PlotSizeCountHelper(plots.Plots.OrEmpty())

	s.plotFilesize.Reset()
	s.plotCount.Reset()

	// Now we can set the gauges with the calculated total values
	// Labels: "size", "type", "compression"
	for kSize, cLevels := range plotSize {
		for cLevel, fileSizes := range cLevels {
			s.plotFilesize.WithLabelValues(fmt.Sprintf("%d", kSize), "og", fmt.Sprintf("%d", cLevel)).Set(float64(fileSizes[PlotTypeOg]))
			s.plotFilesize.WithLabelValues(fmt.Sprintf("%d", kSize), "pool", fmt.Sprintf("%d", cLevel)).Set(float64(fileSizes[PlotTypePool]))
		}
	}

	for kSize, cLevelsByType := range plotCount {
		for cLevel, plotCountByType := range cLevelsByType {
			s.plotCount.WithLabelValues(fmt.Sprintf("%d", kSize), "og", fmt.Sprintf("%d", cLevel)).Set(float64(plotCountByType[PlotTypeOg]))
			s.plotCount.WithLabelValues(fmt.Sprintf("%d", kSize), "pool", fmt.Sprintf("%d", cLevel)).Set(float64(plotCountByType[PlotTypePool]))
		}
	}

	totalPlotCount := len(plots.Plots.OrEmpty())
	s.totalPlots.Set(float64(totalPlotCount))

	s.totalPlotsValue = uint64(totalPlotCount)
}

// PlotSizeCountHelper returns information about plot sizes and counts for the given set of plots
// Return is (plotSize, plotCount)
func PlotSizeCountHelper(plots []protocols.Plot) (map[uint8]map[uint8]map[PlotType]uint64, map[uint8]map[uint8]map[PlotType]uint64) {
	// First, iterate through all the plots to get totals for each ksize
	//          map[ksize]map[clevel]map[PlotType]uint64
	plotSize := map[uint8]map[uint8]map[PlotType]uint64{}
	plotCount := map[uint8]map[uint8]map[PlotType]uint64{}

	for _, plot := range plots {
		cLevel := plot.CompressionLevel.OrElse(uint8(0))
		kSize := plot.Size

		if _, ok := plotSize[kSize]; !ok {
			// It's safe to assume that if plotSize isn't set, plotCount isn't either, since they are created together
			plotSize[kSize] = map[uint8]map[PlotType]uint64{}
			plotCount[kSize] = map[uint8]map[PlotType]uint64{}
		}

		if _, ok := plotSize[kSize][cLevel]; !ok {
			plotSize[kSize][cLevel] = map[PlotType]uint64{
				PlotTypeOg:   0,
				PlotTypePool: 0,
			}
			plotCount[kSize][cLevel] = map[PlotType]uint64{
				PlotTypeOg:   0,
				PlotTypePool: 0,
			}
		}

		if plot.PoolContractPuzzleHash.IsPresent() {
			plotSize[kSize][cLevel][PlotTypePool] += plot.FileSize
			plotCount[kSize][cLevel][PlotTypePool]++
		} else {
			plotSize[kSize][cLevel][PlotTypeOg] += plot.FileSize
			plotCount[kSize][cLevel][PlotTypeOg]++
		}
	}

	return plotSize, plotCount
}
