package metrics

import (
	"log"

	"github.com/cmmarslender/go-chia-rpc/pkg/types"

	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Timelord RPC calls are in this file

// TimelordServiceMetrics contains all metrics related to the crawler
type TimelordServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics
}

// InitMetrics sets all the metrics properties
func (s *TimelordServiceMetrics) InitMetrics() {}

// InitialData is called on startup of the metrics server, to allow seeding metrics with
// current/initial data
func (s *TimelordServiceMetrics) InitialData() {
	utils.LogErr(s.metrics.client.CrawlerService.GetPeerCounts())
}

// ReceiveResponse handles crawler responses that are returned over the websocket
func (s *TimelordServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	//("finished_pot_challenge", "new_compact_proof", "skipping_peak", "new_peak")
	switch resp.Command {
	case "finished_pot_challenge":
		fallthrough
	case "new_compact_proof":
		fallthrough
	case "skipping_peak":
		fallthrough
	case "new_peak":
		log.Printf("%s", string(resp.Data))
	}
}
