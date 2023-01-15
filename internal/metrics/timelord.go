package metrics

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"

	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"

	wrappedPrometheus "github.com/chia-network/chia-exporter/internal/prometheus"
	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Timelord RPC calls are in this file

// TimelordServiceMetrics contains all metrics related to the crawler
type TimelordServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// Timelord Metrics
	fastestTimelord    *wrappedPrometheus.LazyCounter
	slowTimelord       *wrappedPrometheus.LazyCounter
	estimatedIPS       *wrappedPrometheus.LazyGauge
	compactProofsFound *prometheus.CounterVec
}

// InitMetrics sets all the metrics properties
func (s *TimelordServiceMetrics) InitMetrics() {
	s.fastestTimelord = s.metrics.newCounter(chiaServiceTimelord, "fastest_timelord", "Counter for how many times this timelord has been fastest since the exporter has been running")
	s.slowTimelord = s.metrics.newCounter(chiaServiceTimelord, "slow_timelord", "Counter for how many times this timelord has NOT been the fastest since the exporter has been running")
	s.estimatedIPS = s.metrics.newGauge(chiaServiceTimelord, "estimated_ips", "Current estimated IPS. Updated every time a new PoT Challenge is complete")
	s.compactProofsFound = s.metrics.newCounterVec(chiaServiceTimelord, "compact_proofs_completed", "Count of the number of compact proofs by proof type since the exporter was started", []string{"vdf_field"})
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with
// current/initial data
func (s *TimelordServiceMetrics) InitialData() {
	utils.LogErr(s.metrics.client.CrawlerService.GetPeerCounts())
}

// SetupPollingMetrics starts any metrics that happen on an interval
func (s *TimelordServiceMetrics) SetupPollingMetrics() {}

// Disconnected clears/unregisters metrics when the connection drops
func (s *TimelordServiceMetrics) Disconnected() {
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
	//("finished_pot_challenge", "new_compact_proof", "skipping_peak", "new_peak")
	switch resp.Command {
	case "finished_pot":
		s.FinishedPoT(resp)
	case "new_compact_proof":
		s.NewCompactProof(resp)
	case "skipping_peak":
		s.SkippingPeak(resp)
	case "new_peak":
		s.NewPeak(resp)
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
