package metrics

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/cmmarslender/go-chia-rpc/pkg/rpc"
	"github.com/cmmarslender/go-chia-rpc/pkg/types"
	"github.com/prometheus/client_golang/prometheus"

	prometheus2 "github.com/chia-network/chia-exporter/internal/prometheus"
	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Full Node RPC calls are in this file

// FullNodeServiceMetrics contains all metrics related to the full node
type FullNodeServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// GetBlockchainState Metrics
	difficulty    *prometheus2.LazyGauge
	mempoolCost   *prometheus2.LazyGauge
	mempoolMinFee *prometheus.GaugeVec
	mempoolSize   *prometheus2.LazyGauge
	netspaceMiB   *prometheus2.LazyGauge
	nodeHeight    *prometheus2.LazyGauge
	nodeSynced    *prometheus2.LazyGauge

	// BlockCount Metrics
	compactBlocks   *prometheus2.LazyGauge
	uncompactBlocks *prometheus2.LazyGauge
	hintCount       *prometheus2.LazyGauge

	// Connection Metrics
	connectionCount *prometheus.GaugeVec

	// Block Metrics
	maxBlockCost *prometheus2.LazyGauge
	blockCost    *prometheus2.LazyGauge
	blockFees    *prometheus2.LazyGauge
	kSize        *prometheus.CounterVec
}

// InitMetrics sets all the metrics properties
func (s *FullNodeServiceMetrics) InitMetrics() {
	// BlockchainState Metrics
	s.difficulty, _ = s.metrics.newGauge(chiaServiceFullNode, "difficulty", "")
	s.mempoolCost, _ = s.metrics.newGauge(chiaServiceFullNode, "mempool_cost", "")
	s.mempoolMinFee, _ = s.metrics.newGaugeVec(chiaServiceFullNode, "mempool_min_fee", "", []string{"cost"})
	s.mempoolSize, _ = s.metrics.newGauge(chiaServiceFullNode, "mempool_size", "")
	s.netspaceMiB, _ = s.metrics.newGauge(chiaServiceFullNode, "netspace_mib", "")
	s.nodeHeight, _ = s.metrics.newGauge(chiaServiceFullNode, "node_height", "")
	s.nodeSynced, _ = s.metrics.newGauge(chiaServiceFullNode, "node_synced", "")

	// BlockCount Metrics
	s.compactBlocks, _ = s.metrics.newGauge(chiaServiceFullNode, "compact_blocks", "")
	s.uncompactBlocks, _ = s.metrics.newGauge(chiaServiceFullNode, "uncompact_blocks", "")
	s.hintCount, _ = s.metrics.newGauge(chiaServiceFullNode, "hint_count", "")

	// Connection Metrics
	s.connectionCount, _ = s.metrics.newGaugeVec(chiaServiceFullNode, "connection_count", "", []string{"node_type"})

	// Unfinished Block Metrics
	s.maxBlockCost, _ = s.metrics.newGauge(chiaServiceFullNode, "block_max_cost", "")
	s.blockCost, _ = s.metrics.newGauge(chiaServiceFullNode, "block_cost", "")
	s.blockFees, _ = s.metrics.newGauge(chiaServiceFullNode, "block_fees", "")
	s.kSize, _ = s.metrics.newCounterVec(chiaServiceFullNode, "k_size", "", []string{"size"})
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with
// current/initial data
func (s *FullNodeServiceMetrics) InitialData() {
	// Ask for some initial data so we dont have to wait as long
	utils.LogErr(s.metrics.client.FullNodeService.GetBlockchainState()) // Also calls get_connections once we get the response
	utils.LogErr(s.metrics.client.FullNodeService.GetBlockCountMetrics())
}

// ReceiveResponse handles full node related responses that are returned over the websocket
func (s *FullNodeServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	switch resp.Command {
	case "get_blockchain_state":
		s.GetBlockchainState(resp)
		// Ask for connection info when we get updated blockchain state
		utils.LogErr(s.metrics.client.FullNodeService.GetConnections(&rpc.GetConnectionsOptions{}))
	case "block":
		s.Block(resp)
		// Ask for block count metrics when we get a new block
		utils.LogErr(s.metrics.client.FullNodeService.GetBlockCountMetrics())
	case "get_connections":
		s.GetConnections(resp)
	case "get_block_count_metrics":
		s.GetBlockCountMetrics(resp)
	}
}

// GetBlockchainState handler for get_blockchain_state events
func (s *FullNodeServiceMetrics) GetBlockchainState(resp *types.WebsocketResponse) {
	state := &types.WebsocketBlockchainState{}
	err := json.Unmarshal(resp.Data, state)
	if err != nil {
		log.Printf("Error unmarshalling: %s\n", err.Error())
		return
	}

	if state.BlockchainState.Sync.Synced == true {
		s.nodeSynced.Set(1)
	} else {
		s.nodeSynced.Set(0)
	}

	s.nodeHeight.Set(float64(state.BlockchainState.Peak.Height))
	space := state.BlockchainState.Space
	MiB := space.Div64(1048576)
	if MiB.FitsInUint64() {
		s.netspaceMiB.Set(float64(MiB.Uint64()))
	}
	s.difficulty.Set(float64(state.BlockchainState.Difficulty))
	s.mempoolSize.Set(float64(state.BlockchainState.MempoolSize))
	s.mempoolCost.Set(float64(state.BlockchainState.MempoolCost))
	s.mempoolMinFee.WithLabelValues("5000000").Set(float64(state.BlockchainState.MempoolMinFees.Cost5m))
	s.maxBlockCost.Set(float64(state.BlockchainState.BlockMaxCost))
}

// GetConnections handler for get_connections events
func (s *FullNodeServiceMetrics) GetConnections(resp *types.WebsocketResponse) {
	connections := &rpc.GetConnectionsResponse{}
	err := json.Unmarshal(resp.Data, connections)
	if err != nil {
		log.Printf("Error unmarshalling: %s\n", err.Error())
		return
	}

	fullNode := 0.0
	harvester := 0.0
	farmer := 0.0
	timelord := 0.0
	introducer := 0.0
	wallet := 0.0

	for _, connection := range connections.Connections {
		switch connection.Type {
		case types.NodeTypeFullNode:
			fullNode++
		case types.NodeTypeHarvester:
			harvester++
		case types.NodeTypeFarmer:
			farmer++
		case types.NodeTypeTimelord:
			timelord++
		case types.NodeTypeIntroducer:
			introducer++
		case types.NodeTypeWallet:
			wallet++
		}
	}

	s.connectionCount.WithLabelValues("full_node").Set(fullNode)
	s.connectionCount.WithLabelValues("harvester").Set(harvester)
	s.connectionCount.WithLabelValues("farmer").Set(farmer)
	s.connectionCount.WithLabelValues("timelord").Set(timelord)
	s.connectionCount.WithLabelValues("introducer").Set(introducer)
	s.connectionCount.WithLabelValues("wallet").Set(wallet)
}

// Block handler for block events
func (s *FullNodeServiceMetrics) Block(resp *types.WebsocketResponse) {
	block := &types.BlockEvent{}
	err := json.Unmarshal(resp.Data, block)
	if err != nil {
		log.Printf("Error unmarshalling: %s\n", err.Error())
		return
	}

	s.kSize.WithLabelValues(fmt.Sprintf("%d", block.KSize)).Inc()

	if block.TransactionBlock == true {
		s.blockCost.Set(float64(block.BlockCost))
		s.blockFees.Set(float64(block.BlockFees))
	}
}

// GetBlockCountMetrics updates count metrics when we receive a response to get_block_count_metrics
// We ask for this data every time we get a new `block` event
func (s *FullNodeServiceMetrics) GetBlockCountMetrics(resp *types.WebsocketResponse) {
	blockMetrics := &rpc.GetBlockCountMetricsResponse{}
	err := json.Unmarshal(resp.Data, blockMetrics)
	if err != nil {
		log.Printf("Error unmarshalling: %s\n", err.Error())
		return
	}

	s.compactBlocks.Set(float64(blockMetrics.Metrics.CompactBlocks))
	s.uncompactBlocks.Set(float64(blockMetrics.Metrics.UncompactBlocks))
	s.hintCount.Set(float64(blockMetrics.Metrics.HintCount))
}
