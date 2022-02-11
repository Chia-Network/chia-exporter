package metrics

import (
	"encoding/json"
	"log"

	"github.com/cmmarslender/go-chia-rpc/pkg/rpc"
	"github.com/cmmarslender/go-chia-rpc/pkg/types"
	"github.com/prometheus/client_golang/prometheus"

	prometheus2 "github.com/chia-network/chia-exporter/internal/prometheus"
	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Crawler RPC calls are in this file

// CrawlerServiceMetrics contains all metrics related to the crawler
type CrawlerServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// Crawler Metrics
	totalNodes5Days *prometheus2.LazyGauge
	reliableNodes   *prometheus2.LazyGauge
	ipv4Nodes5Days  *prometheus2.LazyGauge
	ipv6Nodes5Days  *prometheus2.LazyGauge
	versionBuckets  *prometheus.GaugeVec
}

// InitMetrics sets all the metrics properties
func (s *CrawlerServiceMetrics) InitMetrics() {
	// Crawler Metrics
	s.totalNodes5Days, _ = s.metrics.newGauge(chiaServiceCrawler, "total_nodes_5_days", "")
	s.reliableNodes, _ = s.metrics.newGauge(chiaServiceCrawler, "reliable_nodes", "reliable nodes are nodes that have port 8444 open and have available space for more peer connections")
	s.ipv4Nodes5Days, _ = s.metrics.newGauge(chiaServiceCrawler, "ipv4_nodes_5_days", "")
	s.ipv6Nodes5Days, _ = s.metrics.newGauge(chiaServiceCrawler, "ipv6_nodes_5_days", "")
	s.versionBuckets, _ = s.metrics.newGaugeVec(chiaServiceCrawler, "version_bucket", "", []string{"version"})
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with
// current/initial data
func (s *CrawlerServiceMetrics) InitialData() {
	utils.LogErr(s.metrics.client.CrawlerService.GetPeerCounts())
}

// ReceiveResponse handles crawler responses that are returned over the websocket
func (s *CrawlerServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	switch resp.Command {
	case "get_peer_counts":
		fallthrough
	case "loaded_initial_peers":
		fallthrough
	case "crawl_batch_completed":
		s.GetPeerCounts(resp)
	}
}

// GetPeerCounts handles a response from get_peer_counts
func (s *CrawlerServiceMetrics) GetPeerCounts(resp *types.WebsocketResponse) {
	counts := &rpc.GetPeerCountsResponse{}
	err := json.Unmarshal(resp.Data, counts)
	if err != nil {
		log.Printf("Error unmarshalling: %s\n", err.Error())
		return
	}

	s.totalNodes5Days.Set(float64(counts.PeerCounts.TotalLast5Days))
	s.reliableNodes.Set(float64(counts.PeerCounts.ReliableNodes))
	s.ipv4Nodes5Days.Set(float64(counts.PeerCounts.IPV4Last5Days))
	s.ipv6Nodes5Days.Set(float64(counts.PeerCounts.IPV6Last5Days))

	for version, count := range counts.PeerCounts.Versions {
		s.versionBuckets.WithLabelValues(version).Set(float64(count))
	}
}
