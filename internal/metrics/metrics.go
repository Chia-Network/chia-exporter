package metrics

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-sql-driver/mysql"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/chia-network/go-chia-libs/pkg/rpc"
	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	wrappedPrometheus "github.com/chia-network/go-modules/pkg/prometheus"
)

type chiaService string

const (
	chiaServiceFullNode  chiaService = "full_node"
	chiaServiceWallet    chiaService = "wallet"
	chiaServiceCrawler   chiaService = "crawler"
	chiaServiceTimelord  chiaService = "timelord"
	chiaServiceHarvester chiaService = "harvester"
	chiaServiceFarmer    chiaService = "farmer"
)

// serviceMetrics defines methods that must be on all metrics services
type serviceMetrics interface {
	// InitMetrics registers any metrics (gauges, counters, etc) on creation of the metrics object
	InitMetrics(network *string)

	// InitialData is called after the websocket connection is opened to allow each service
	// to load any initial data that should be reported
	InitialData()

	// SetupPollingMetrics Some services need data that doesn't have a good event to hook into
	// In those cases, we have to fall back to polling
	SetupPollingMetrics()

	// ReceiveResponse is called when a response is received for the particular metrics service
	ReceiveResponse(*types.WebsocketResponse)

	// Disconnected is called when the websocket is disconnected, to clear metrics, etc
	Disconnected()

	// Reconnected is called when the websocket is reconnected after a disconnection
	Reconnected()
}

// Metrics is the main entrypoint
type Metrics struct {
	metricsPort uint16
	client      *rpc.Client
	network     *string
	lastReceive time.Time

	// httpClient is another instance of the rpc.Client in HTTP mode
	// This is used rarely, to request data in response to a websocket event that is too large to fit on a single
	// websocket connection or needs to be paginated
	httpClient *rpc.Client

	// This holds a custom prometheus registry so that only our metrics are exported, and not the default go metrics
	registry           *prometheus.Registry
	dynamicPromHandler *dynamicPromHandler

	// Holds a MySQL DB Instance if configured
	mysqlClient *sql.DB

	// All the serviceMetrics interfaces that are registered
	serviceMetrics map[chiaService]serviceMetrics
}

// NewMetrics returns a new instance of metrics
// All metrics are registered here
func NewMetrics(port uint16, logLevel log.Level) (*Metrics, error) {
	var err error

	metrics := &Metrics{
		metricsPort:    port,
		registry:       prometheus.NewRegistry(),
		serviceMetrics: map[chiaService]serviceMetrics{},
	}

	log.SetLevel(logLevel)

	err = metrics.setNewClient()
	if err != nil {
		return nil, err
	}

	metrics.httpClient, err = rpc.NewClient(rpc.ConnectionModeHTTP, rpc.WithAutoConfig(), rpc.WithBaseURL(&url.URL{
		Scheme: "https",
		Host:   viper.GetString("hostname"),
	}), rpc.WithTimeout(viper.GetDuration("rpc-timeout")))
	if err != nil {
		// For now, http client is optional
		// Sometimes this fails with outdated config.yaml files that don't have the crawler/seeder section present
		log.Errorf("Error creating http client: %s\n", err.Error())
	}

	err = metrics.createDBClient()
	if err != nil {
		log.Debugf("Error creating MySQL Client. Will not store any metrics to MySQL. %s\n", err.Error())
	}
	err = metrics.initTables()
	if err != nil {
		log.Debugf("Error ensuring tables exist in MySQL. Will not process or store any MySQL only metrics: %s\n", err.Error())
		metrics.mysqlClient = nil
	}

	// Register each service's metrics
	metrics.serviceMetrics[chiaServiceFullNode] = &FullNodeServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chiaServiceWallet] = &WalletServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chiaServiceCrawler] = &CrawlerServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chiaServiceTimelord] = &TimelordServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chiaServiceHarvester] = &HarvesterServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chiaServiceFarmer] = &FarmerServiceMetrics{metrics: metrics}

	// See if we can get the network now
	// If not, the reconnect handler will handle it later
	_, _ = metrics.checkNetwork()

	// Init each service's metrics
	for _, service := range metrics.serviceMetrics {
		service.InitMetrics(metrics.network)
	}

	return metrics, nil
}

func (m *Metrics) setNewClient() error {
	m.client = nil
	client, err := rpc.NewClient(rpc.ConnectionModeWebsocket, rpc.WithAutoConfig(), rpc.WithBaseURL(&url.URL{
		Scheme: "wss",
		Host:   viper.GetString("hostname"),
	}))
	if err != nil {
		return err
	}
	m.client = client
	return nil
}

func (m *Metrics) createDBClient() error {
	var err error

	cfg := mysql.Config{
		User:                 viper.GetString("mysql-user"),
		Passwd:               viper.GetString("mysql-password"),
		Net:                  "tcp",
		Addr:                 fmt.Sprintf("%s:%d", viper.GetString("mysql-host"), viper.GetUint16("mysql-port")),
		DBName:               viper.GetString("mysql-db-name"),
		AllowNativePasswords: true,
	}
	m.mysqlClient, err = sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}

	m.mysqlClient.SetConnMaxLifetime(time.Minute * 3)
	m.mysqlClient.SetMaxOpenConns(10)
	m.mysqlClient.SetMaxIdleConns(10)

	return nil
}

// Returns boolean indicating if network changed from previously known value
func (m *Metrics) checkNetwork() (bool, error) {
	var currentNetwork string
	var newNetwork string
	if m.network == nil {
		currentNetwork = ""
	} else {
		currentNetwork = *m.network
	}

	m.client.SetSyncMode()
	defer m.client.SetAsyncMode()

	netInfo, _, err := m.client.DaemonService.GetNetworkInfo(&rpc.GetNetworkInfoOptions{})
	if err != nil {
		return false, fmt.Errorf("error checking network info: %w", err)
	}

	if netInfo != nil && netInfo.NetworkName.IsPresent() {
		network := netInfo.NetworkName.MustGet()
		m.network = &network
		newNetwork = network
	} else {
		m.network = nil
		newNetwork = ""
	}

	if currentNetwork != newNetwork {
		return true, nil
	}

	return false, nil
}

// newGauge returns a lazy gauge that follows naming conventions
func (m *Metrics) newGauge(service chiaService, name string, help string) *wrappedPrometheus.LazyGauge {
	opts := prometheus.GaugeOpts{
		Namespace: "chia",
		Subsystem: string(service),
		Name:      name,
		Help:      help,
	}

	if m.network != nil {
		opts.ConstLabels = map[string]string{
			"network": *m.network,
		}
	}

	gm := prometheus.NewGauge(opts)

	lg := &wrappedPrometheus.LazyGauge{
		Gauge:    gm,
		Registry: m.registry,
	}

	return lg
}

// newGauge returns a gaugeVec that follows naming conventions and registers it with the prometheus collector
// This doesn't need a lazy wrapper, as they're inherently lazy registered for each label value provided
func (m *Metrics) newGaugeVec(service chiaService, name string, help string, labels []string) *prometheus.GaugeVec {
	opts := prometheus.GaugeOpts{
		Namespace: "chia",
		Subsystem: string(service),
		Name:      name,
		Help:      help,
	}

	if m.network != nil {
		opts.ConstLabels = map[string]string{
			"network": *m.network,
		}
	}

	gm := prometheus.NewGaugeVec(opts, labels)

	m.registry.MustRegister(gm)

	return gm
}

// newGauge returns a counter that follows naming conventions and registers it with the prometheus collector
func (m *Metrics) newCounter(service chiaService, name string, help string) *wrappedPrometheus.LazyCounter {
	opts := prometheus.CounterOpts{
		Namespace: "chia",
		Subsystem: string(service),
		Name:      name,
		Help:      help,
	}

	if m.network != nil {
		opts.ConstLabels = map[string]string{
			"network": *m.network,
		}
	}

	cm := prometheus.NewCounter(opts)

	lc := &wrappedPrometheus.LazyCounter{
		Counter:  cm,
		Registry: m.registry,
	}

	return lc
}

// newCounterVec returns a counter that follows naming conventions and registers it with the prometheus collector
func (m *Metrics) newCounterVec(service chiaService, name string, help string, labels []string) *prometheus.CounterVec {
	opts := prometheus.CounterOpts{
		Namespace: "chia",
		Subsystem: string(service),
		Name:      name,
		Help:      help,
	}

	if m.network != nil {
		opts.ConstLabels = map[string]string{
			"network": *m.network,
		}
	}

	gm := prometheus.NewCounterVec(opts, labels)

	m.registry.MustRegister(gm)

	return gm
}

// OpenWebsocket sets up the RPC client and subscribes to relevant topics
func (m *Metrics) OpenWebsocket() error {
	err := m.client.SubscribeSelf()
	if err != nil {
		return err
	}

	err = m.client.Subscribe("metrics")
	if err != nil {
		return err
	}

	_, err = m.client.AddHandler(m.websocketReceive)
	if err != nil {
		return err
	}

	m.client.AddDisconnectHandler(m.disconnectHandler)
	m.client.AddReconnectHandler(m.reconnectHandler)

	// First, we check the network and see if it changed
	// If changed, we completely replace the prometheus registry with a new registry and re-init all metrics
	// otherwise, we call the reconnected handlers
	changed, _ := m.checkNetwork()
	if changed {
		m.registry = prometheus.NewRegistry()
		m.dynamicPromHandler.updateHandler(promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
		// Init each service's metrics
		for _, service := range m.serviceMetrics {
			service.InitMetrics(m.network)
		}
	}

	for _, service := range m.serviceMetrics {
		service.InitialData()
		service.SetupPollingMetrics()
	}

	m.lastReceive = time.Now()
	go func() {
		for {
			// If we don't get any events for 5 minutes, we'll reset the connection
			time.Sleep(10 * time.Second)

			if m.lastReceive.Before(time.Now().Add(-5 * time.Minute)) {
				log.Info("Websocket connection seems down. Recreating...")
				m.disconnectHandler()
				err := m.setNewClient()
				if err != nil {
					log.Errorf("Error creating new client: %s", err.Error())
					continue
				}

				err = m.OpenWebsocket()
				if err != nil {
					log.Errorf("Error opening websocket on new client: %s", err.Error())
					continue
				}

				// Got the new connection open, so stop the loop on the old connection
				// since we called this function again and a new loop was created
				break
			}
		}
	}()

	return nil
}

// CloseWebsocket closes the websocket connection
func (m *Metrics) CloseWebsocket() error {
	// @TODO reenable once fixed in the upstream dep
	//return m.client.DaemonService.CloseConnection()
	return nil
}

// StartServer starts the metrics server
func (m *Metrics) StartServer() error {
	log.Printf("Starting metrics server on port %d", m.metricsPort)

	m.dynamicPromHandler = &dynamicPromHandler{}
	m.dynamicPromHandler.updateHandler(promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	http.Handle("/metrics", m.dynamicPromHandler)
	http.HandleFunc("/healthz", healthcheckEndpoint)
	return http.ListenAndServe(fmt.Sprintf(":%d", m.metricsPort), nil)
}

func (m *Metrics) websocketReceive(resp *types.WebsocketResponse, err error) {
	if err != nil {
		log.Errorf("Websocket received err: %s\n", err.Error())
		return
	}

	m.lastReceive = time.Now()

	log.Printf("recv: %s %s\n", resp.Origin, resp.Command)
	log.Debugf("origin: %s command: %s destination: %s data: %s\n", resp.Origin, resp.Command, resp.Destination, string(resp.Data))

	switch resp.Origin {
	case "chia_full_node":
		m.serviceMetrics[chiaServiceFullNode].ReceiveResponse(resp)
	case "chia_wallet":
		m.serviceMetrics[chiaServiceWallet].ReceiveResponse(resp)
	case "chia_crawler":
		m.serviceMetrics[chiaServiceCrawler].ReceiveResponse(resp)
	case "chia_timelord":
		m.serviceMetrics[chiaServiceTimelord].ReceiveResponse(resp)
	case "chia_harvester":
		m.serviceMetrics[chiaServiceHarvester].ReceiveResponse(resp)
	case "chia_farmer":
		m.serviceMetrics[chiaServiceFarmer].ReceiveResponse(resp)
	}
}

func (m *Metrics) disconnectHandler() {
	log.Debug("Calling disconnect handlers")
	for _, service := range m.serviceMetrics {
		service.Disconnected()
	}
}

func (m *Metrics) reconnectHandler() {
	log.Debug("Calling reconnect handlers")

	// First, we check the network and see if it changed
	// If changed, we completely replace the prometheus registry with a new registry and re-init all metrics
	// otherwise, we call the reconnected handlers
	changed, err := m.checkNetwork()
	if changed || err != nil {
		if err != nil {
			m.network = nil
			log.Errorf("Error checking network. Assuming network changed and resetting metrics: %s\n", err.Error())
		}

		log.Info("Network Changed, resetting all metrics")
		m.registry = prometheus.NewRegistry()
		m.dynamicPromHandler.updateHandler(promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
		// Init each service's metrics
		for _, service := range m.serviceMetrics {
			service.InitMetrics(m.network)
			service.InitialData()
		}
	} else {
		log.Debug("Network did not change")
		for _, service := range m.serviceMetrics {
			service.Reconnected()
		}
	}
}

// Healthcheck endpoint for metrics server
func healthcheckEndpoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, err := fmt.Fprintf(w, "Ok")
	if err != nil {
		log.Errorf("Error writing healthcheck response %s\n", err.Error())
	}
}

func connectionCountHelper(resp *types.WebsocketResponse, connectionCount *prometheus.GaugeVec) {
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

	if conns, hasConns := connections.Connections.Get(); hasConns {
		for _, connection := range conns {
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

	connectionCount.WithLabelValues("full_node").Set(fullNode)
	connectionCount.WithLabelValues("harvester").Set(harvester)
	connectionCount.WithLabelValues("farmer").Set(farmer)
	connectionCount.WithLabelValues("timelord").Set(timelord)
	connectionCount.WithLabelValues("introducer").Set(introducer)
	connectionCount.WithLabelValues("wallet").Set(wallet)
}

type debugEvent struct {
	Data map[string]float64 `json:"data"`
}

// debugHelper handles debug events
// Expects map[string]number - where number is able to be parsed into a float64 type
// Assigns the key (string) as the "key" label on the metric, and passes the value straight through
func debugHelper(resp *types.WebsocketResponse, debugGaugeVec *prometheus.GaugeVec) {
	debugMetrics := debugEvent{}
	err := json.Unmarshal(resp.Data, &debugMetrics)
	if err != nil {
		log.Errorf("Error unmarshalling debugMetrics: %s\n", err.Error())
		return
	}

	for key, value := range debugMetrics.Data {
		debugGaugeVec.WithLabelValues(key).Set(value)
	}
}
