package metrics

import (
	"encoding/json"
	"fmt"

	"github.com/chia-network/go-chia-libs/pkg/rpc"
	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"

	wrappedPrometheus "github.com/chia-network/go-modules/pkg/prometheus"

	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Farmer RPC calls are in this file

// FarmerServiceMetrics contains all metrics related to the harvester
type FarmerServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// Connection Metrics
	connectionCount *prometheus.GaugeVec

	// Keep a local copy of the plot count, so we can do other actions when the value changes
	// Tracked per node ID, since we get checkins from all harvesters here
	totalPlotsValue map[types.Bytes32]uint64

	// Also have to keep track of node id to hostname mapping, since not all responses have the friendly hostname
	nodeIDToHostname map[types.Bytes32]string

	// Partial/Pooling Metrics
	submittedPartials   *prometheus.CounterVec
	currentDifficulty   *prometheus.GaugeVec
	pointsAckSinceStart *prometheus.GaugeVec

	// Proof Metrics
	proofsFound *wrappedPrometheus.LazyCounter

	// Remote Harvester Plot Counts
	plotFilesize       *prometheus.GaugeVec
	plotCount          *prometheus.GaugeVec
	totalFoundProofs   *prometheus.CounterVec
	lastFoundProofs    *prometheus.GaugeVec
	totalEligiblePlots *prometheus.CounterVec
	lastEligiblePlots  *prometheus.GaugeVec
	lastLookupTime     *prometheus.GaugeVec

	// Debug Metric
	debug *prometheus.GaugeVec

	// Tracking certain requests to make sure only one happens at any given time
	gettingHarvesters bool
}

// InitMetrics sets all the metrics properties
func (s *FarmerServiceMetrics) InitMetrics() {
	s.totalPlotsValue = map[types.Bytes32]uint64{}
	s.nodeIDToHostname = map[types.Bytes32]string{}

	// Connection Metrics
	s.connectionCount = s.metrics.newGaugeVec(chiaServiceFarmer, "connection_count", "Number of active connections for each type of peer", []string{"node_type"})

	// Partial/Pooling Metrics, by launcher ID
	poolLabels := []string{"launcher_id"}
	s.submittedPartials = s.metrics.newCounterVec(chiaServiceFarmer, "submitted_partials", "Number of partials submitted since the exporter was started", poolLabels)
	s.currentDifficulty = s.metrics.newGaugeVec(chiaServiceFarmer, "current_difficulty", "Current difficulty for this launcher id", poolLabels)
	s.pointsAckSinceStart = s.metrics.newGaugeVec(chiaServiceFarmer, "points_acknowledged_since_start", "Points acknowledged since start. This is calculated by chia, NOT since start of the exporter.", poolLabels)

	// Proof Metrics
	s.proofsFound = s.metrics.newCounter(chiaServiceFarmer, "proofs_found", "Number of proofs found since the exporter has been running")

	// Remote harvester plot counts
	plotLabels := []string{"host", "node_id", "size", "type", "compression"}
	s.plotFilesize = s.metrics.newGaugeVec(chiaServiceFarmer, "plot_filesize", "Filesize of plots separated by harvester", plotLabels)
	s.plotCount = s.metrics.newGaugeVec(chiaServiceFarmer, "plot_count", "Number of plots separated by harvester", plotLabels)
	s.totalFoundProofs = s.metrics.newCounterVec(chiaServiceFarmer, "total_found_proofs", "Counter of total found proofs since the exporter started", []string{"host", "node_id"})
	s.lastFoundProofs = s.metrics.newGaugeVec(chiaServiceFarmer, "last_found_proofs", "Number of proofs found for the last farmer_info event", []string{"host", "node_id"})
	s.totalEligiblePlots = s.metrics.newCounterVec(chiaServiceFarmer, "total_eligible_plots", "Counter of total eligible plots since the exporter started", []string{"host", "node_id"})
	s.lastEligiblePlots = s.metrics.newGaugeVec(chiaServiceFarmer, "last_eligible_plots", "Number of eligible plots for the last farmer_info event", []string{"host", "node_id"})
	s.lastLookupTime = s.metrics.newGaugeVec(chiaServiceFarmer, "last_lookup_time", "Lookup time for the last farmer_info event", []string{"host", "node_id"})

	// Debug Metric
	s.debug = s.metrics.newGaugeVec(chiaServiceFarmer, "debug_metrics", "random debugging metrics distinguished by labels", []string{"key"})
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with current/initial data
func (s *FarmerServiceMetrics) InitialData() {
	utils.LogErr(s.metrics.client.FarmerService.GetConnections(&rpc.GetConnectionsOptions{}))
}

// SetupPollingMetrics starts any metrics that happen on an interval
func (s *FarmerServiceMetrics) SetupPollingMetrics() {}

// Disconnected clears/unregisters metrics when the connection drops
func (s *FarmerServiceMetrics) Disconnected() {
	s.connectionCount.Reset()
	s.plotFilesize.Reset()
	s.plotCount.Reset()
	s.lastFoundProofs.Reset()
	s.lastEligiblePlots.Reset()
	s.lastLookupTime.Reset()
}

// Reconnected is called when the service is reconnected after the websocket was disconnected
func (s *FarmerServiceMetrics) Reconnected() {
	s.InitialData()
}

// ReceiveResponse handles crawler responses that are returned over the websocket
func (s *FarmerServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	switch resp.Command {
	case "get_connections":
		s.GetConnections(resp)
	case "new_farming_info":
		s.NewFarmingInfo(resp)
	case "submitted_partial":
		s.SubmittedPartial(resp)
	case "proof":
		s.Proof(resp)
	case "harvester_removed":
		fallthrough
	case "add_connection":
		fallthrough
	case "close_connection":
		utils.LogErr(s.metrics.client.FarmerService.GetConnections(&rpc.GetConnectionsOptions{}))
	case "debug":
		debugHelper(resp, s.debug)
	}
}

// GetConnections handler for get_connections events
func (s *FarmerServiceMetrics) GetConnections(resp *types.WebsocketResponse) {
	connectionCountHelper(resp, s.connectionCount)
	s.GetHarvesters()
}

func (s *FarmerServiceMetrics) GetHarvesters() {
	if s.gettingHarvesters {
		log.Debug("Skipping get_harvesters since another request is already in flight")
		return
	}
	s.gettingHarvesters = true
	defer func() {
		s.gettingHarvesters = false
	}()

	harvesters, _, err := s.metrics.httpClient.FarmerService.GetHarvesters(&rpc.FarmerGetHarvestersOptions{})
	if err != nil {
		log.Errorf("farmer: Error getting harvesters: %s\n", err.Error())
		return
	}

	// Must be reset prior to setting new values in case all of a particular type, k-size, or c level are gone
	s.plotFilesize.Reset()
	s.plotCount.Reset()

	for _, harvester := range harvesters.Harvesters {
		// keep track of the node ID to host mapping
		s.nodeIDToHostname[harvester.Connection.NodeID] = harvester.Connection.Host

		_totalPlotCount := uint64(0)
		plotSize, plotCount := PlotSizeCountHelper(harvester.Plots)

		// Now we can set the gauges with the calculated total values
		// Labels: "host", "size", "type", "compression"
		for kSize, cLevels := range plotSize {
			for cLevel, fileSizes := range cLevels {
				s.plotFilesize.WithLabelValues(harvester.Connection.Host, harvester.Connection.NodeID.String(), fmt.Sprintf("%d", kSize), "og", fmt.Sprintf("%d", cLevel)).Set(float64(fileSizes[PlotTypeOg]))
				s.plotFilesize.WithLabelValues(harvester.Connection.Host, harvester.Connection.NodeID.String(), fmt.Sprintf("%d", kSize), "pool", fmt.Sprintf("%d", cLevel)).Set(float64(fileSizes[PlotTypePool]))
			}
		}

		for kSize, cLevelsByType := range plotCount {
			for cLevel, plotCountByType := range cLevelsByType {
				_totalPlotCount += plotCountByType[PlotTypeOg]
				_totalPlotCount += plotCountByType[PlotTypePool]

				s.plotCount.WithLabelValues(harvester.Connection.Host, harvester.Connection.NodeID.String(), fmt.Sprintf("%d", kSize), "og", fmt.Sprintf("%d", cLevel)).Set(float64(plotCountByType[PlotTypeOg]))
				s.plotCount.WithLabelValues(harvester.Connection.Host, harvester.Connection.NodeID.String(), fmt.Sprintf("%d", kSize), "pool", fmt.Sprintf("%d", cLevel)).Set(float64(plotCountByType[PlotTypePool]))
			}
		}

		s.totalPlotsValue[harvester.Connection.NodeID] = _totalPlotCount
	}
}

// NewFarmingInfo handles new_farming_info events
func (s *FarmerServiceMetrics) NewFarmingInfo(resp *types.WebsocketResponse) {
	info := &types.EventFarmerNewFarmingInfo{}
	err := json.Unmarshal(resp.Data, info)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	nodeID := info.FarmingInfo.NodeID
	hostname, foundHostname := s.nodeIDToHostname[nodeID]
	if !foundHostname || s.totalPlotsValue[nodeID] != uint64(info.FarmingInfo.TotalPlots) {
		log.Debugf("Missing node ID to host mapping or plot count doesn't match. Refreshing harvester info. New Plot Count: %d | Previous Plot Count: %d\n", info.FarmingInfo.TotalPlots, s.totalPlotsValue[nodeID])
		// When plot counts change, we have to refresh information about the plots
		utils.LogErr(s.metrics.client.FarmerService.GetConnections(&rpc.GetConnectionsOptions{}))
	}

	if foundHostname {
		// Labels: "host", "node_id"
		s.totalFoundProofs.WithLabelValues(hostname, nodeID.String()).Add(float64(info.FarmingInfo.Proofs))
		s.lastFoundProofs.WithLabelValues(hostname, nodeID.String()).Set(float64(info.FarmingInfo.Proofs))
		s.totalEligiblePlots.WithLabelValues(hostname, nodeID.String()).Add(float64(info.FarmingInfo.PassedFilter))
		s.lastEligiblePlots.WithLabelValues(hostname, nodeID.String()).Set(float64(info.FarmingInfo.PassedFilter))
		s.lastLookupTime.WithLabelValues(hostname, nodeID.String()).Set(float64(info.FarmingInfo.LookupTime))
	}

}

// SubmittedPartial handles a received submitted_partial event
func (s *FarmerServiceMetrics) SubmittedPartial(resp *types.WebsocketResponse) {
	partial := &types.EventFarmerSubmittedPartial{}
	err := json.Unmarshal(resp.Data, partial)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	s.submittedPartials.WithLabelValues(partial.LauncherID.String()).Inc()
	s.currentDifficulty.WithLabelValues(partial.LauncherID.String()).Set(float64(partial.CurrentDifficulty))
	s.pointsAckSinceStart.WithLabelValues(partial.LauncherID.String()).Set(float64(partial.PointsAcknowledgedSinceStart))
}

// Proof handles a received `proof` event from the farmer
func (s *FarmerServiceMetrics) Proof(resp *types.WebsocketResponse) {
	proof := &types.EventFarmerProof{}
	err := json.Unmarshal(resp.Data, proof)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	s.proofsFound.Inc()
}
