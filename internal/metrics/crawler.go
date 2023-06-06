package metrics

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
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
	maxMindCountryDB *maxminddb.Reader
	maxMindASNDB     *maxminddb.Reader

	// Crawler Metrics
	totalNodes5Days         *wrappedPrometheus.LazyGauge
	reliableNodes           *wrappedPrometheus.LazyGauge
	ipv4Nodes5Days          *wrappedPrometheus.LazyGauge
	ipv6Nodes5Days          *wrappedPrometheus.LazyGauge
	versionBuckets          *prometheus.GaugeVec
	countryNodeCountBuckets *prometheus.GaugeVec
	asnNodeCountBuckets     *prometheus.GaugeVec
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
	s.asnNodeCountBuckets = s.metrics.newGaugeVec(chiaServiceCrawler, "asn_node_count", "Number of peers gossiped in the last 5 days from each asn.", []string{"asn", "organization"})

	err := s.initMaxmindCountryDB()
	if err != nil {
		// Continue on maxmind error - optional/not critical functionality
		log.Printf("Error initializing maxmind country DB: %s\n", err.Error())
	}

	err = s.initMaxmindASNDB()
	if err != nil {
		// Continue on maxmind error - optional/not critical functionality
		log.Printf("Error initializing maxmind ASN DB: %s\n", err.Error())
	}
}

// initMaxmindCountryDB loads the maxmind country DB if the file is present
// If the DB is not present, ip/country mapping is skipped
func (s *CrawlerServiceMetrics) initMaxmindCountryDB() error {
	var err error
	dbPath := viper.GetString("maxmind-country-db-path")
	if dbPath == "" {
		return nil
	}
	s.maxMindCountryDB, err = maxminddb.Open(dbPath)
	if err != nil {
		return err
	}

	return nil
}

// initMaxmindASNDB loads the maxmind ASN DB if the file is present
// If the DB is not present, ip/ASN mapping is skipped
func (s *CrawlerServiceMetrics) initMaxmindASNDB() error {
	var err error
	dbPath := "GeoLite2-ASN.mmdb"
	if dbPath == "" {
		return nil
	}
	s.maxMindASNDB, err = maxminddb.Open(dbPath)
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

		s.StartIPMapping(counts.PeerCounts.TotalLast5Days)
	}
}

// StartIPMapping starts the process to fetch current IPs from the crawler
// when a response is received, the IPs are mapped to countries and/or ASNs using maxmind, if databases are provided
// Updates metrics value once all pages have been received
func (s *CrawlerServiceMetrics) StartIPMapping(limit uint) {
	// If we don't have either maxmind DB, bail now
	if s.maxMindCountryDB == nil && s.maxMindASNDB == nil {
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
	// If we don't have either maxmind DB, bail now
	if s.maxMindCountryDB == nil && s.maxMindASNDB == nil {
		return
	}

	ips := &rpc.GetIPsAfterTimestampResponse{}
	err := json.Unmarshal(resp.Data, ips)
	if err != nil {
		log.Printf("Error unmarshalling: %s\n", err.Error())
		return
	}

	s.ProcessIPCountryMapping(ips)
	s.ProcessIPASNMapping(ips)
}

// ProcessIPCountryMapping Processes the list of IPs to countries
func (s *CrawlerServiceMetrics) ProcessIPCountryMapping(ips *rpc.GetIPsAfterTimestampResponse) {
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

// ProcessIPASNMapping Processes the list of IPs to ASNs
func (s *CrawlerServiceMetrics) ProcessIPASNMapping(ips *rpc.GetIPsAfterTimestampResponse) {
	type countStruct struct {
		ASN          int
		Organization string
		Count        float64
	}
	asnCounts := map[int]*countStruct{}

	ipfile, err := os.Create("ips.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer ipfile.Close()
	ipfile.WriteString("ip\n")

	for _, ip := range ips.IPs {
		ipfile.WriteString(fmt.Sprintf("%s\n", ip))
		asn, err := s.GetASNForIP(ip)
		if err != nil {
			continue
		}

		if _, ok := asnCounts[asn.AutonomousSystemNumber]; !ok {
			asnCounts[asn.AutonomousSystemNumber] = &countStruct{
				ASN:          asn.AutonomousSystemNumber,
				Organization: asn.AutonomousSystemOrganization,
				Count:        0,
			}
		}

		asnCounts[asn.AutonomousSystemNumber].Count++
	}

	asnfile, err := os.Create("by-asn.csv")
	if err != nil {
		log.Fatal(err)
	}
	defer asnfile.Close()

	asnfile.WriteString(fmt.Sprintf("\"%s\",\"%s\",\"%s\"\n", "asn", "organization", "count"))

	for _, asnData := range asnCounts {
		asnfile.WriteString(fmt.Sprintf("\"%d\",\"%s\",\"%f\"\n", asnData.ASN, asnData.Organization, asnData.Count))
		s.asnNodeCountBuckets.WithLabelValues(fmt.Sprintf("%d", asnData.ASN), asnData.Organization).Set(asnData.Count)
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
	if s.maxMindCountryDB == nil {
		return nil, fmt.Errorf("maxmind country DB not initialized")
	}

	ip := net.ParseIP(ipStr)

	record := &CountryRecord{}

	err := s.maxMindCountryDB.Lookup(ip, record)
	if err != nil {
		return nil, err
	}

	return record, nil
}

// ASNRecord record of a country from maxmind
type ASNRecord struct {
	AutonomousSystemNumber       int    `maxminddb:"autonomous_system_number"`
	AutonomousSystemOrganization string `maxminddb:"autonomous_system_organization"`
}

// GetASNForIP Gets ASN data for an ip address
func (s *CrawlerServiceMetrics) GetASNForIP(ipStr string) (*ASNRecord, error) {
	if s.maxMindASNDB == nil {
		return nil, fmt.Errorf("maxmind ASN DB not initialized")
	}

	ip := net.ParseIP(ipStr)

	record := &ASNRecord{}

	err := s.maxMindASNDB.Lookup(ip, &record)
	if err != nil {
		return nil, err
	}

	return record, nil
}
