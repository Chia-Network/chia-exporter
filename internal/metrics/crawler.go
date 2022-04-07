package metrics

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/chia-network/go-chia-libs/pkg/rpc"
	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/viper"

	"github.com/oschwald/maxminddb-golang"

	wrappedPrometheus "github.com/chia-network/chia-exporter/internal/prometheus"
	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Crawler RPC calls are in this file

// CrawlerServiceMetrics contains all metrics related to the crawler
type CrawlerServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// Interfaces with Maxmind
	maxMindDB *maxminddb.Reader

	// Crawler Metrics
	totalNodes5Days         *wrappedPrometheus.LazyGauge
	reliableNodes           *wrappedPrometheus.LazyGauge
	ipv4Nodes5Days          *wrappedPrometheus.LazyGauge
	ipv6Nodes5Days          *wrappedPrometheus.LazyGauge
	versionBuckets          *prometheus.GaugeVec
	countryNodeCountBuckets *prometheus.GaugeVec
}

// InitMetrics sets all the metrics properties
func (s *CrawlerServiceMetrics) InitMetrics() {
	// Crawler Metrics
	s.totalNodes5Days = s.metrics.newGauge(chiaServiceCrawler, "total_nodes_5_days", "Total number of nodes that have been gossiped around the network with a timestamp in the last 5 days. The crawler did not necessarily connect to all of these peers itself.")
	s.reliableNodes = s.metrics.newGauge(chiaServiceCrawler, "reliable_nodes", "reliable nodes are nodes that have port 8444 open and have available space for more peer connections")
	s.ipv4Nodes5Days = s.metrics.newGauge(chiaServiceCrawler, "ipv4_nodes_5_days", "Total number of IPv4 nodes that have been gossiped around the network with a timestamp in the last 5 days. The crawler did not necessarily connect to all of these peers itself.")
	s.ipv6Nodes5Days = s.metrics.newGauge(chiaServiceCrawler, "ipv6_nodes_5_days", "Total number of IPv6 nodes that have been gossiped around the network with a timestamp in the last 5 days. The crawler did not necessarily connect to all of these peers itself.")
	s.versionBuckets = s.metrics.newGaugeVec(chiaServiceCrawler, "peer_version", "Number of peers for each version. Only peers the crawler was able to connect to are included here.", []string{"version"})
	s.countryNodeCountBuckets = s.metrics.newGaugeVec(chiaServiceCrawler, "country_node_count", "Number of peers gossiped in the last 5 days from each country.", []string{"country", "country_display"})

	err := s.initMaxmindDB()
	if err != nil {
		// Continue on maxmind error - optional/not critical functionality
		log.Printf("Error initializing maxmind DB: %s\n", err.Error())
	}
}

// initMaxmindDB loads the maxmind DB if the file is present
// If the DB is not present, ip/country mapping is skipped
func (s *CrawlerServiceMetrics) initMaxmindDB() error {
	var err error
	dbPath := viper.GetString("maxmind-country-db-path")
	if dbPath == "" {
		return nil
	}
	s.maxMindDB, err = maxminddb.Open(dbPath)
	if err != nil {
		return err
	}

	return nil
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with current/initial data
func (s *CrawlerServiceMetrics) InitialData() {}

// Disconnected clears/unregisters metrics when the connection drops
func (s *CrawlerServiceMetrics) Disconnected() {
	s.totalNodes5Days.Unregister()
	s.reliableNodes.Unregister()
	s.ipv4Nodes5Days.Unregister()
	s.ipv6Nodes5Days.Unregister()
	s.versionBuckets.Reset()
	s.countryNodeCountBuckets.Reset()
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
	case "get_ips_after_timestamp":
		s.GetIPsAfterTimestamp(resp)
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

	if counts.PeerCounts != nil {
		s.totalNodes5Days.Set(float64(counts.PeerCounts.TotalLast5Days))
		s.reliableNodes.Set(float64(counts.PeerCounts.ReliableNodes))
		s.ipv4Nodes5Days.Set(float64(counts.PeerCounts.IPV4Last5Days))
		s.ipv6Nodes5Days.Set(float64(counts.PeerCounts.IPV6Last5Days))

		for version, count := range counts.PeerCounts.Versions {
			s.versionBuckets.WithLabelValues(version).Set(float64(count))
		}

		s.StartIPCountryMapping(counts.PeerCounts.TotalLast5Days)
	}
}

// StartIPCountryMapping starts the process to fetch current IPs from the crawler
// and maps them to countries using maxmind
// Updates metrics value once all pages have been received
func (s *CrawlerServiceMetrics) StartIPCountryMapping(limit uint) {
	if s.maxMindDB == nil {
		return
	}

	utils.LogErr(
		s.metrics.client.CrawlerService.GetIPsAfterTimestamp(&rpc.GetIPsAfterTimestampOptions{
			After: time.Now().Add(-5 * time.Hour * 24).Unix(),
			Limit: limit,
		}),
	)
}

// GetIPsAfterTimestamp processes a response of IPs seen since a timestamp
// Currently assumes all IPs will be in one response
func (s *CrawlerServiceMetrics) GetIPsAfterTimestamp(resp *types.WebsocketResponse) {
	if s.maxMindDB == nil {
		return
	}

	ips := &rpc.GetIPsAfterTimestampResponse{}
	err := json.Unmarshal(resp.Data, ips)
	if err != nil {
		log.Printf("Error unmarshalling: %s\n", err.Error())
		return
	}

	type countStruct struct {
		ISOCode string
		Name    string
		Count   float64
	}
	countryCounts := map[string]*countStruct{}

	for _, ip := range ips.IPs {
		country, err := s.GetCountryForIP(ip)
		if err != nil || country.Country.ISOCode == "" {
			continue
		}

		countryName := ""
		countryName, _ = country.Country.Names["en"]

		if _, ok := countryCounts[country.Country.ISOCode]; !ok {
			countryCounts[country.Country.ISOCode] = &countStruct{
				ISOCode: country.Country.ISOCode,
				Name:    countryName,
				Count:   0,
			}
		}

		countryCounts[country.Country.ISOCode].Count++
	}

	for _, countryData := range countryCounts {
		s.countryNodeCountBuckets.WithLabelValues(countryData.ISOCode, countryData.Name).Set(countryData.Count)
	}
}

// CountryRecord record of a country from maxmind
type CountryRecord struct {
	Country Country `maxminddb:"country"`
}

// Country record of country data from maxmind record
type Country struct {
	ISOCode string            `maxminddb:"iso_code"`
	Names   map[string]string `maxminddb:"names"`
}

// GetCountryForIP Gets country data for an ip address
func (s *CrawlerServiceMetrics) GetCountryForIP(ipStr string) (*CountryRecord, error) {
	if s.maxMindDB == nil {
		return nil, fmt.Errorf("maxmind not initialized")
	}

	ip := net.ParseIP(ipStr)

	record := &CountryRecord{}

	err := s.maxMindDB.Lookup(ip, record)
	if err != nil {
		return nil, err
	}

	return record, nil
}
