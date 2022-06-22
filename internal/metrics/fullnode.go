package metrics

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/chia-network/go-chia-libs/pkg/config"
	log "github.com/sirupsen/logrus"

	"github.com/chia-network/go-chia-libs/pkg/rpc"
	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"

	wrappedPrometheus "github.com/chia-network/chia-exporter/internal/prometheus"
	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Full Node RPC calls are in this file

// FullNodeServiceMetrics contains all metrics related to the full node
type FullNodeServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// GetBlockchainState Metrics
	difficulty          *wrappedPrometheus.LazyGauge
	mempoolCost         *wrappedPrometheus.LazyGauge
	mempoolMinFee       *prometheus.GaugeVec
	mempoolSize         *wrappedPrometheus.LazyGauge
	mempoolMaxTotalCost *wrappedPrometheus.LazyGauge
	netspaceMiB         *wrappedPrometheus.LazyGauge
	nodeHeight          *wrappedPrometheus.LazyGauge
	nodeHeightSynced    *wrappedPrometheus.LazyGauge
	nodeSynced          *wrappedPrometheus.LazyGauge

	// BlockCount Metrics
	compactBlocks   *wrappedPrometheus.LazyGauge
	uncompactBlocks *wrappedPrometheus.LazyGauge
	hintCount       *wrappedPrometheus.LazyGauge

	// Connection Metrics
	connectionCount *prometheus.GaugeVec

	// Block Metrics
	maxBlockCost      *wrappedPrometheus.LazyGauge
	blockCost         *wrappedPrometheus.LazyGauge
	blockFees         *wrappedPrometheus.LazyGauge
	kSize             *prometheus.CounterVec
	preValidationTime *wrappedPrometheus.LazyGauge
	validationTime    *wrappedPrometheus.LazyGauge

	// Signage Point Metrics
	totalSignagePoints   *wrappedPrometheus.LazyCounter
	signagePointsSubSlot *wrappedPrometheus.LazyGauge
	currentSignagePoint  *wrappedPrometheus.LazyGauge

	// Filesize Metrics
	database          *wrappedPrometheus.LazyGauge
	databaseWal       *wrappedPrometheus.LazyGauge
	databaseShm       *wrappedPrometheus.LazyGauge
	peersDat          *wrappedPrometheus.LazyGauge
	heightToHash      *wrappedPrometheus.LazyGauge
	subEpochSummaries *wrappedPrometheus.LazyGauge
}

// InitMetrics sets all the metrics properties
func (s *FullNodeServiceMetrics) InitMetrics() {
	// BlockchainState Metrics
	s.difficulty = s.metrics.newGauge(chiaServiceFullNode, "difficulty", "Current network difficulty")
	s.mempoolCost = s.metrics.newGauge(chiaServiceFullNode, "mempool_cost", "Current mempool size in cost")
	s.mempoolMinFee = s.metrics.newGaugeVec(chiaServiceFullNode, "mempool_min_fee", "Minimum fee to get into the mempool, in fee per cost, for a particular transaction cost", []string{"cost"})
	s.mempoolSize = s.metrics.newGauge(chiaServiceFullNode, "mempool_size", "Number of spends in the mempool")
	s.mempoolMaxTotalCost = s.metrics.newGauge(chiaServiceFullNode, "mempool_max_total_cost", "The maximum capacity of the mempool, in cost")
	s.netspaceMiB = s.metrics.newGauge(chiaServiceFullNode, "netspace_mib", "Current estimated netspace, in MiB")
	s.nodeHeight = s.metrics.newGauge(chiaServiceFullNode, "node_height", "Current height of the node")
	s.nodeHeightSynced = s.metrics.newGauge(chiaServiceFullNode, "node_height_synced", "Current height of the node, when synced. This will register/unregister automatically depending on sync state, and should help make rate() more sane, when you don't want rate of syncing, only rate of the chain.")
	s.nodeSynced = s.metrics.newGauge(chiaServiceFullNode, "node_synced", "Indicates whether this node is currently synced")

	// BlockCount Metrics
	s.compactBlocks = s.metrics.newGauge(chiaServiceFullNode, "compact_blocks", "Number of fully compact blocks in this node's database")
	s.uncompactBlocks = s.metrics.newGauge(chiaServiceFullNode, "uncompact_blocks", "Number of uncompact blocks in this node's database")
	s.hintCount = s.metrics.newGauge(chiaServiceFullNode, "hint_count", "Number of hints in this nodes database")

	// Connection Metrics
	s.connectionCount = s.metrics.newGaugeVec(chiaServiceFullNode, "connection_count", "Number of active connections for each type of peer", []string{"node_type"})

	// Unfinished Block Metrics
	s.maxBlockCost = s.metrics.newGauge(chiaServiceFullNode, "block_max_cost", "Max block size, in cost")
	s.blockCost = s.metrics.newGauge(chiaServiceFullNode, "block_cost", "Total cost of all transactions in the last block")
	s.blockFees = s.metrics.newGauge(chiaServiceFullNode, "block_fees", "Total fees in the last block")
	s.kSize = s.metrics.newCounterVec(chiaServiceFullNode, "k_size", "Counts of winning plot size since the exporter was last started", []string{"size"})
	s.preValidationTime = s.metrics.newGauge(chiaServiceFullNode, "pre_validation_time", "Last pre_validation_time from the block event")
	s.validationTime = s.metrics.newGauge(chiaServiceFullNode, "validation_time", "Last validation time from the block event")

	// Signage Point Metrics
	s.totalSignagePoints = s.metrics.newCounter(chiaServiceFullNode, "total_signage_points", "Total number of signage points since the metrics exporter started. Only useful when combined with rate() or similar")
	s.signagePointsSubSlot = s.metrics.newGauge(chiaServiceFullNode, "signage_points_sub_slot", "Number of signage points per sub slot")
	s.currentSignagePoint = s.metrics.newGauge(chiaServiceFullNode, "current_signage_point", "Index of the last signage point received")

	// File Size Metrics
	s.database = s.metrics.newGauge(chiaServiceFullNode, "database_filesize", "Size of the database file")
	s.databaseWal = s.metrics.newGauge(chiaServiceFullNode, "database_wal_filesize", "Size of the database wal file")
	s.databaseShm = s.metrics.newGauge(chiaServiceFullNode, "database_shm_filesize", "Size of the database shm file")
	s.peersDat = s.metrics.newGauge(chiaServiceFullNode, "peers_dat_filesize", "Size of peers.dat file")
	s.heightToHash = s.metrics.newGauge(chiaServiceFullNode, "height_to_hash_filesize", "Size of height_to_hash file")
	s.subEpochSummaries = s.metrics.newGauge(chiaServiceFullNode, "sub_epoch_summaries_filesize", "Size of sub_epoch_summaries file")
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with
// current/initial data
func (s *FullNodeServiceMetrics) InitialData() {
	// Ask for some initial data so we dont have to wait as long
	utils.LogErr(s.metrics.client.FullNodeService.GetBlockchainState()) // Also calls get_connections once we get the response
	utils.LogErr(s.metrics.client.FullNodeService.GetBlockCountMetrics())

	// Things that update in the background
	go func() {
		for {
			s.RefreshFileSizes()
			time.Sleep(30 * time.Second)
		}
	}()
}

// Disconnected clears/unregisters metrics when the connection drops
func (s *FullNodeServiceMetrics) Disconnected() {
	s.difficulty.Unregister()
	s.mempoolCost.Unregister()
	s.mempoolMinFee.Reset()
	s.mempoolSize.Unregister()
	s.mempoolMaxTotalCost.Unregister()
	s.netspaceMiB.Unregister()
	s.nodeHeight.Unregister()
	s.nodeHeightSynced.Unregister()
	s.nodeSynced.Unregister()

	s.compactBlocks.Unregister()
	s.uncompactBlocks.Unregister()
	s.hintCount.Unregister()

	s.connectionCount.Reset()

	s.maxBlockCost.Unregister()
	s.blockCost.Unregister()
	s.blockFees.Unregister()
	s.kSize.Reset()

	s.totalSignagePoints.Unregister()
	s.signagePointsSubSlot.Unregister()
	s.currentSignagePoint.Unregister()
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
	case "signage_point":
		s.SignagePoint(resp)
	}
}

// GetBlockchainState handler for get_blockchain_state events
func (s *FullNodeServiceMetrics) GetBlockchainState(resp *types.WebsocketResponse) {
	state := &types.WebsocketBlockchainState{}
	err := json.Unmarshal(resp.Data, state)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	if state.BlockchainState.Sync != nil {
		if state.BlockchainState.Sync.Synced {
			s.nodeSynced.Set(1)
		} else {
			s.nodeSynced.Set(0)
		}
	}

	if state.BlockchainState.Peak != nil {
		s.nodeHeight.Set(float64(state.BlockchainState.Peak.Height))
		if state.BlockchainState.Sync.Synced {
			s.nodeHeightSynced.Set(float64(state.BlockchainState.Peak.Height))
		} else {
			s.nodeHeightSynced.Unregister()
		}
	}

	space := state.BlockchainState.Space
	MiB := space.Div64(1048576)
	if MiB.FitsInUint64() {
		s.netspaceMiB.Set(float64(MiB.Uint64()))
	}
	s.difficulty.Set(float64(state.BlockchainState.Difficulty))
	s.mempoolSize.Set(float64(state.BlockchainState.MempoolSize))
	s.mempoolCost.Set(float64(state.BlockchainState.MempoolCost))
	s.mempoolMaxTotalCost.Set(float64(state.BlockchainState.MempoolMaxTotalCost))
	if state.BlockchainState.MempoolMinFees != nil {
		s.mempoolMinFee.WithLabelValues("5000000").Set(float64(state.BlockchainState.MempoolMinFees.Cost5m))
	}
	s.maxBlockCost.Set(float64(state.BlockchainState.BlockMaxCost))
}

// GetConnections handler for get_connections events
func (s *FullNodeServiceMetrics) GetConnections(resp *types.WebsocketResponse) {
	connections := &rpc.GetConnectionsResponse{}
	err := json.Unmarshal(resp.Data, connections)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	fullNode := 0.0
	harvester := 0.0
	farmer := 0.0
	timelord := 0.0
	introducer := 0.0
	wallet := 0.0

	for _, connection := range connections.Connections {
		if connection != nil {
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
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	s.kSize.WithLabelValues(fmt.Sprintf("%d", block.KSize)).Inc()
	s.preValidationTime.Set(block.PreValidationTime)
	s.validationTime.Set(block.ValidationTime)

	if block.TransactionBlock {
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
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	if blockMetrics.Metrics != nil {
		s.compactBlocks.Set(float64(blockMetrics.Metrics.CompactBlocks))
		s.uncompactBlocks.Set(float64(blockMetrics.Metrics.UncompactBlocks))
		s.hintCount.Set(float64(blockMetrics.Metrics.HintCount))
	}
}

// SignagePoint handles signage point metrics
func (s *FullNodeServiceMetrics) SignagePoint(resp *types.WebsocketResponse) {
	signagePoint := &types.SignagePointEvent{}
	err := json.Unmarshal(resp.Data, signagePoint)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	// total signage current
	s.totalSignagePoints.Inc()
	s.signagePointsSubSlot.Set(float64(64))
	s.currentSignagePoint.Set(float64(signagePoint.BroadcastFarmer.SignagePointIndex))
}

// RefreshFileSizes periodically checks how large files related to the full node are
func (s *FullNodeServiceMetrics) RefreshFileSizes() {
	log.Info("cron: chia_full_node updating file sizes")
	cfg, err := config.GetChiaConfig()
	if err != nil {
		log.Errorf("Error getting chia config: %s\n", err.Error())
	}
	database := cfg.GetFullPath(cfg.FullNode.DatabasePath)
	databaseWal := fmt.Sprintf("%s-wal", database)
	databaseShm := fmt.Sprintf("%s-shm", database)
	heightToHash := cfg.GetFullPath("db/height-to-hash")
	peersDat := cfg.GetFullPath("db/peers.dat")
	subEpochSummaries := cfg.GetFullPath("db/sub-epoch-summaries")

	utils.LogErr(nil, nil, setGaugeToFilesize(database, s.database))
	utils.LogErr(nil, nil, setGaugeToFilesize(databaseWal, s.databaseWal))
	utils.LogErr(nil, nil, setGaugeToFilesize(databaseShm, s.databaseShm))
	utils.LogErr(nil, nil, setGaugeToFilesize(heightToHash, s.heightToHash))
	utils.LogErr(nil, nil, setGaugeToFilesize(peersDat, s.peersDat))
	utils.LogErr(nil, nil, setGaugeToFilesize(subEpochSummaries, s.subEpochSummaries))
}

func setGaugeToFilesize(file string, g *wrappedPrometheus.LazyGauge) error {
	log.Debugf("file: chia_full_node Getting filesize of %s\n", file)
	fi, err := os.Stat(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Debugf("file: chia_full_node file doesn't exist: %s\n", file)
			return nil
		} else {
			return err
		}

	}

	g.Set(float64(fi.Size()))

	return nil
}
