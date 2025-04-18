package metrics

import (
	"context"
	"encoding/json"

	"github.com/chia-network/go-chia-libs/pkg/rpc"
	log "github.com/sirupsen/logrus"

	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"

	wrappedPrometheus "github.com/chia-network/go-modules/pkg/prometheus"

	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Timelord RPC calls are in this file

// TimelordServiceMetrics contains all metrics related to the crawler
type TimelordServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// General Service Metrics
	gotVersionResponse bool
	version *prometheus.GaugeVec

	// Timelord Metrics
	fastestTimelord    *wrappedPrometheus.LazyCounter
	slowTimelord       *wrappedPrometheus.LazyCounter
	estimatedIPS       *wrappedPrometheus.LazyGauge
	compactProofsFound *prometheus.CounterVec

	// Debug Metric
	debug *prometheus.GaugeVec
}

// InitMetrics sets all the metrics properties
func (s *TimelordServiceMetrics) InitMetrics(network *string) {
	// General Service Metrics
	s.version = s.metrics.newGaugeVec(chiaServiceTimelord, "version", "The version of chia-blockchain the service is running", []string{"version"})

	s.fastestTimelord = s.metrics.newCounter(chiaServiceTimelord, "fastest_timelord", "Counter for how many times this timelord has been fastest since the exporter has been running")
	s.slowTimelord = s.metrics.newCounter(chiaServiceTimelord, "slow_timelord", "Counter for how many times this timelord has NOT been the fastest since the exporter has been running")
	s.estimatedIPS = s.metrics.newGauge(chiaServiceTimelord, "estimated_ips", "Current estimated IPS. Updated every time a new PoT Challenge is complete")
	s.compactProofsFound = s.metrics.newCounterVec(chiaServiceTimelord, "compact_proofs_completed", "Count of the number of compact proofs by proof type since the exporter was started", []string{"vdf_field"})

	// Debug Metric
	s.debug = s.metrics.newGaugeVec(chiaServiceTimelord, "debug_metrics", "random debugging metrics distinguished by labels", []string{"key"})
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with
// current/initial data
func (s *TimelordServiceMetrics) InitialData() {
	// Only get the version on an initial or reconnection
	utils.LogErr(s.metrics.client.TimelordService.GetVersion(&rpc.GetVersionOptions{}))
}

// SetupPollingMetrics starts any metrics that happen on an interval
func (s *TimelordServiceMetrics) SetupPollingMetrics(ctx context.Context) {}

// Disconnected clears/unregisters metrics when the connection drops
func (s *TimelordServiceMetrics) Disconnected() {
	s.version.Reset()
	s.gotVersionResponse = false
	s.fastestTimelord.Unregister()
	s.slowTimelord.Unregister()
	s.estimatedIPS.Unregister()
	s.compactProofsFound.Reset()
}

// Reconnected is called when the service is reconnected after the websocket was disconnected
func (s *TimelordServiceMetrics) Reconnected() {
	s.InitialData()
}

// ReceiveResponse handles crawler responses that are returned over the websocket
func (s *TimelordServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	// Sometimes, when we reconnect, or start exporter before chia is running
	// the daemon is up before the service, and the initial request for the version
	// doesn't make it to the service
	// daemon doesn't queue these messages for later, they just get dropped
	if !s.gotVersionResponse {
		utils.LogErr(s.metrics.client.FullNodeService.GetVersion(&rpc.GetVersionOptions{}))
	}

	//("finished_pot_challenge", "new_compact_proof", "skipping_peak", "new_peak")
	switch resp.Command {
	case "get_version":
		versionHelper(resp, s.version)
		s.gotVersionResponse = true
	case "finished_pot":
		s.FinishedPoT(resp)
	case "new_compact_proof":
		s.NewCompactProof(resp)
	case "skipping_peak":
		s.SkippingPeak(resp)
	case "new_peak":
		s.NewPeak(resp)
	case "debug":
		debugHelper(resp, s.debug)
	}
}

// FinishedPoT Handles new PoT Challenge Events
func (s *TimelordServiceMetrics) FinishedPoT(resp *types.WebsocketResponse) {
	potEvent := &types.FinishedPoTEvent{}
	err := json.Unmarshal(resp.Data, potEvent)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}
	s.estimatedIPS.Set(potEvent.EstimatedIPS)
}

// NewCompactProof Handles new compact proof events
func (s *TimelordServiceMetrics) NewCompactProof(resp *types.WebsocketResponse) {
	compactProof := &types.NewCompactProofEvent{}
	err := json.Unmarshal(resp.Data, compactProof)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	var field string
	switch compactProof.FieldVdf {
	case types.CompressibleVDFFieldCCEOSVDF:
		field = "CC_EOS_VDF"
	case types.CompressibleVDFFieldICCEOSVDF:
		field = "ICC_EOS_VDF"
	case types.CompressibleVDFFieldCCSPVDF:
		field = "CC_SP_VDF"
	case types.CompressibleVDFFieldCCIPVDF:
		field = "CC_IP_VDF"
	default:
		return
	}

	s.compactProofsFound.WithLabelValues(field).Inc()
}

// SkippingPeak Fastest!
func (s *TimelordServiceMetrics) SkippingPeak(resp *types.WebsocketResponse) {
	s.fastestTimelord.Inc()
}

// NewPeak Not the fastest :(
func (s *TimelordServiceMetrics) NewPeak(resp *types.WebsocketResponse) {
	s.slowTimelord.Inc()
}
