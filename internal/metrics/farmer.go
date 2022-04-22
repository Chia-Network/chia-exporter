package metrics

import (
	"github.com/chia-network/go-chia-libs/pkg/types"
)

// Metrics that are based on Farmer RPC calls are in this file

// FarmerServiceMetrics contains all metrics related to the harvester
type FarmerServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics
}

// InitMetrics sets all the metrics properties
func (s *FarmerServiceMetrics) InitMetrics() {

}

// InitialData is called on startup of the metrics server, to allow seeding metrics with current/initial data
func (s *FarmerServiceMetrics) InitialData() {}

// Disconnected clears/unregisters metrics when the connection drops
func (s *FarmerServiceMetrics) Disconnected() {

}

// ReceiveResponse handles crawler responses that are returned over the websocket
func (s *FarmerServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	switch resp.Command {
	case "submitted_partial":
		// @TODO
	case "proof":
		// @TODO
	}
}
