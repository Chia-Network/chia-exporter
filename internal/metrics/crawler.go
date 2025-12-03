package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/chia-network/go-chia-libs/pkg/rpc"
	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/viper"

	"github.com/oschwald/maxminddb-golang"

	wrappedPrometheus "github.com/chia-network/go-modules/pkg/prometheus"

	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Crawler RPC calls are in this file

// CrawlerServiceMetrics contains all metrics related to the crawler
type CrawlerServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// General Service Metrics
	gotVersionResponse bool
	version            *prometheus.GaugeVec

	// Current network
	network *string

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

	// Debug Metric
	debug *prometheus.GaugeVec
}

// InitMetrics sets all the metrics properties
func (s *CrawlerServiceMetrics) InitMetrics(network *string) {
	s.network = network

	// General Service Metrics
	s.version = s.metrics.newGaugeVec(chiaServiceCrawler, "version", "The version of chia-blockchain the service is running", []string{"version"})

	// Crawler Metrics
	s.totalNodes5Days = s.metrics.newGauge(chiaServiceCrawler, "total_nodes_5_days", "Total number of nodes that have been gossiped around the network with a timestamp in the last 5 days. The crawler did not necessarily connect to all of these peers itself.")
	s.reliableNodes = s.metrics.newGauge(chiaServiceCrawler, "reliable_nodes", "reliable nodes are nodes that have port 8444 open and have available space for more peer connections")
	s.ipv4Nodes5Days = s.metrics.newGauge(chiaServiceCrawler, "ipv4_nodes_5_days", "Total number of IPv4 nodes that have been gossiped around the network with a timestamp in the last 5 days. The crawler did not necessarily connect to all of these peers itself.")
	s.ipv6Nodes5Days = s.metrics.newGauge(chiaServiceCrawler, "ipv6_nodes_5_days", "Total number of IPv6 nodes that have been gossiped around the network with a timestamp in the last 5 days. The crawler did not necessarily connect to all of these peers itself.")
	s.versionBuckets = s.metrics.newGaugeVec(chiaServiceCrawler, "peer_version", "Number of peers for each version. Only peers the crawler was able to connect to are included here.", []string{"version"})
	s.countryNodeCountBuckets = s.metrics.newGaugeVec(chiaServiceCrawler, "country_node_count", "Number of peers gossiped in the last 5 days from each country.", []string{"country", "country_display"})

	// Debug Metric
	s.debug = s.metrics.newGaugeVec(chiaServiceCrawler, "debug_metrics", "random debugging metrics distinguished by labels", []string{"key"})

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
	dbPath := viper.GetString("maxmind-asn-db-path")
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
func (s *CrawlerServiceMetrics) InitialData() {
	// Only get the version on an initial or reconnection
	utils.LogErr(s.metrics.client.CrawlerService.GetVersion(&rpc.GetVersionOptions{}))

	utils.LogErr(s.metrics.client.CrawlerService.GetPeerCounts())
}

// SetupPollingMetrics starts any metrics that happen on an interval
func (s *CrawlerServiceMetrics) SetupPollingMetrics(ctx context.Context) {}

// Disconnected clears/unregisters metrics when the connection drops
func (s *CrawlerServiceMetrics) Disconnected() {
	s.version.Reset()
	s.gotVersionResponse = false
	s.totalNodes5Days.Unregister()
	s.reliableNodes.Unregister()
	s.ipv4Nodes5Days.Unregister()
	s.ipv6Nodes5Days.Unregister()
	s.versionBuckets.Reset()
	s.countryNodeCountBuckets.Reset()
}

// Reconnected is called when the service is reconnected after the websocket was disconnected
func (s *CrawlerServiceMetrics) Reconnected() {
	s.InitialData()
}

// ReceiveResponse handles crawler responses that are returned over the websocket
func (s *CrawlerServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	// Sometimes, when we reconnect, or start exporter before chia is running
	// the daemon is up before the service, and the initial request for the version
	// doesn't make it to the service
	// daemon doesn't queue these messages for later, they just get dropped
	if !s.gotVersionResponse {
		utils.LogErr(s.metrics.client.FullNodeService.GetVersion(&rpc.GetVersionOptions{}))
	}

	switch resp.Command {
	case "get_version":
		versionHelper(resp, s.version)
		s.gotVersionResponse = true
	case "get_peer_counts":
		fallthrough
	case "loaded_initial_peers":
		fallthrough
	case "crawl_batch_completed":
		s.GetPeerCounts(resp)
	case "debug":
		debugHelper(resp, s.debug)
	}
}

// GetPeerCounts handles a response from get_peer_counts
func (s *CrawlerServiceMetrics) GetPeerCounts(resp *types.WebsocketResponse) {
	counts := &rpc.GetPeerCountsResponse{}
	err := json.Unmarshal(resp.Data, counts)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	if peerCounts, hasPeerCounts := counts.PeerCounts.Get(); hasPeerCounts {
		s.totalNodes5Days.Set(float64(peerCounts.TotalLast5Days))
		s.reliableNodes.Set(float64(peerCounts.ReliableNodes))
		s.ipv4Nodes5Days.Set(float64(peerCounts.IPV4Last5Days))
		s.ipv6Nodes5Days.Set(float64(peerCounts.IPV6Last5Days))

		for version, count := range peerCounts.Versions {
			s.versionBuckets.WithLabelValues(version).Set(float64(count))
		}

		s.StartIPMapping(peerCounts.TotalLast5Days)
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

	if s.metrics.httpClient == nil {
		log.Println("httpClient is nil, skipping IP mapping")
		return
	}

	log.Println("Requesting IP addresses from the past 5 days for country and/or ASN mapping...")

	ipsAfterTimestamp, _, err := s.metrics.httpClient.CrawlerService.GetIPsAfterTimestamp(&rpc.GetIPsAfterTimestampOptions{
		After: time.Now().Add(-5 * time.Hour * 24).Unix(),
		Limit: limit,
	})
	if err != nil {
		log.Errorf("Error getting IPs: %s\n", err.Error())
		return
	}

	s.GetIPsAfterTimestamp(ipsAfterTimestamp)
}

// GetIPsAfterTimestamp processes a response of IPs seen since a timestamp
// Currently assumes all IPs will be in one response
func (s *CrawlerServiceMetrics) GetIPsAfterTimestamp(ips *rpc.GetIPsAfterTimestampResponse) {
	// If we don't have either maxmind DB, bail now
	if s.maxMindCountryDB == nil && s.maxMindASNDB == nil {
		log.Debug("Missing both ASN and Country maxmind DBs")
		return
	}

	if ips == nil {
		log.Debug("IPs after timestamp are nil")
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

	if ipresult, hasIPResult := ips.IPs.Get(); hasIPResult {
		for _, ip := range ipresult {
			country, err := s.GetCountryForIP(ip)
			if err != nil || country.Country.ISOCode == "" {
				continue
			}

			countryName := ""
			countryName = country.Country.Names["en"]

			if _, ok := countryCounts[country.Country.ISOCode]; !ok {
				countryCounts[country.Country.ISOCode] = &countStruct{
					ISOCode: country.Country.ISOCode,
					Name:    countryName,
					Count:   0,
				}
			}

			countryCounts[country.Country.ISOCode].Count++
		}
	}

	for _, countryData := range countryCounts {
		s.countryNodeCountBuckets.WithLabelValues(countryData.ISOCode, countryData.Name).Set(countryData.Count)
	}
}

// ProcessIPASNMapping Processes the list of IPs to ASNs
func (s *CrawlerServiceMetrics) ProcessIPASNMapping(ips *rpc.GetIPsAfterTimestampResponse) {
	// Don't process if we can't store
	if s.metrics.mysqlClient == nil {
		log.Debug("MySQL client is nil")
		return
	}
	if s.network == nil {
		log.Errorln("Network information missing. Can't store ASN data without network")
		return
	}

	type countStruct struct {
		ASN          uint32
		Organization string
		Count        uint32
	}
	asnCounts := map[uint32]*countStruct{}

	log.Debugf("Have %d IPs\n", len(ips.IPs.MustGet()))

	if ipresult, hasIPResult := ips.IPs.Get(); hasIPResult {
		for _, ip := range ipresult {
			asn, err := s.GetASNForIP(ip)
			if err != nil {
				log.Debugf("Unable to get ASN for IP %s\n", ip)
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
	}

	err := s.metrics.DeleteASNRecords(*s.network)
	if err != nil {
		log.Errorf("unable to delete old ASN records from the database: %s\n", err.Error())
		return
	}

	batchSize := viper.GetUint32("mysql-batch-size")
	var valueStrings []string
	var valueArgs []interface{}

	for i, asnData := range asnCounts {
		if asnData.ASN == 0 {
			continue
		}

		valueStrings = append(valueStrings, "(?, ?, ?, ?)")
		valueArgs = append(valueArgs, asnData.ASN, asnData.Organization, asnData.Count, *s.network)

		// Execute the batch insert when reaching the batch size or the end of the slice
		if (i+1)%batchSize == 0 || i+1 == uint32(len(asnCounts)) {
			_, err = s.metrics.mysqlClient.Exec(
				fmt.Sprintf("INSERT INTO asn(asn, organization, count, network) VALUES %s", strings.Join(valueStrings, ",")),
				valueArgs...)

			if err != nil {
				log.Errorf("error inserting ASN record to mysql for asn:%d count:%d error: %s\n", asnData.ASN, asnData.Count, err.Error())
			}

			// Reset the slices for the next batch
			valueStrings = []string{}
			valueArgs = []interface{}{}
		}
	}

	// If there are any records left that did not meet the batch size threshold, insert them now
	if len(valueStrings) > 0 {
		_, err := s.metrics.mysqlClient.Exec(
			fmt.Sprintf("INSERT INTO asn(asn, organization, count, network) VALUES %s", strings.Join(valueStrings, ",")),
			valueArgs...)

		if err != nil {
			log.Errorf("error inserting remaining ASN records to mysql: %s\n", err.Error())
		}
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
	AutonomousSystemNumber       uint32 `maxminddb:"autonomous_system_number"`
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
