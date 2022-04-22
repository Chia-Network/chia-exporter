package metrics

import (
	"github.com/chia-network/go-chia-libs/pkg/types"
)

// Metrics that are based on Harvester RPC calls are in this file

// HarvesterServiceMetrics contains all metrics related to the harvester
type HarvesterServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics
}

// InitMetrics sets all the metrics properties
func (s *HarvesterServiceMetrics) InitMetrics() {

}

// InitialData is called on startup of the metrics server, to allow seeding metrics with current/initial data
func (s *HarvesterServiceMetrics) InitialData() {}

// Disconnected clears/unregisters metrics when the connection drops
func (s *HarvesterServiceMetrics) Disconnected() {

}

// ReceiveResponse handles crawler responses that are returned over the websocket
func (s *HarvesterServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	switch resp.Command {
	case "farming_info":
		// @TODO
	}
}
