package metrics

import (
	"fmt"
	"log"
	"net/http"

	"github.com/chia-network/go-chia-libs/pkg/rpc"
	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	wrappedPrometheus "github.com/chia-network/chia-exporter/internal/prometheus"
)

type chiaService string

const (
	chiaServiceFullNode chiaService = "full_node"
	chiaServiceWallet   chiaService = "wallet"
	chiaServiceCrawler  chiaService = "crawler"
	chiaServiceTimelord chiaService = "timelord"
)

// serviceMetrics defines methods that must be on all metrics services
type serviceMetrics interface {
	// InitMetrics registers any metrics (gauges, counters, etc) on creation of the metrics object
	InitMetrics()

	// InitialData is called after the websocket connection is opened to allow each service
	// to load any initial data that should be reported
	InitialData()

	// ReceiveResponse is called when a response is received for the particular metrics service
	ReceiveResponse(*types.WebsocketResponse)

	// Disconnected is called when the websocket is disconnected, to clear metrics, etc
	Disconnected()
}

// Metrics is the main entrypoint
type Metrics struct {
	metricsPort uint16
	client      *rpc.Client

	// httpClient is another instance of the rpc.Client in HTTP mode
	// This is used rarely, to request data in response to a websocket event that is too large to fit on a single
	// websocket connection or needs to be paginated
	httpClient  *rpc.Client

	// This holds a custom prometheus registry so that only our metrics are exported, and not the default go metrics
	registry *prometheus.Registry

	// All the serviceMetrics interfaces that are registered
	serviceMetrics map[chiaService]serviceMetrics
}

// NewMetrics returns a new instance of metrics
// All metrics are registered here
func NewMetrics(port uint16) (*Metrics, error) {
	var err error

	metrics := &Metrics{
		metricsPort:    port,
		registry:       prometheus.NewRegistry(),
		serviceMetrics: map[chiaService]serviceMetrics{},
	}

	metrics.client, err = rpc.NewClient(rpc.ConnectionModeWebsocket)
	if err != nil {
		return nil, err
	}

	metrics.httpClient, err = rpc.NewClient(rpc.ConnectionModeHTTP)
	if err != nil {
		return nil, err
	}

	// Register each service's metrics

	metrics.serviceMetrics[chiaServiceFullNode] = &FullNodeServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chiaServiceWallet] = &WalletServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chiaServiceCrawler] = &CrawlerServiceMetrics{metrics: metrics}
	metrics.serviceMetrics[chiaServiceTimelord] = &TimelordServiceMetrics{metrics: metrics}

	// Init each service's metrics
	for _, service := range metrics.serviceMetrics {
		service.InitMetrics()
	}

	return metrics, nil
}

// newGauge returns a lazy gauge that follows naming conventions
func (m *Metrics) newGauge(service chiaService, name string, help string) *wrappedPrometheus.LazyGauge {
	opts := prometheus.GaugeOpts{
		Namespace: "chia",
		Subsystem: string(service),
		Name:      name,
		Help:      help,
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

	err = m.client.AddHandler(m.websocketReceive)
	if err != nil {
		return err
	}

	m.client.AddDisconnectHandler(m.disconnectHandler)

	for _, service := range m.serviceMetrics {
		service.InitialData()
	}

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

	http.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	http.HandleFunc("/healthz", healthcheckEndpoint)
	return http.ListenAndServe(fmt.Sprintf(":%d", m.metricsPort), nil)
}

func (m *Metrics) websocketReceive(resp *types.WebsocketResponse, err error) {
	if err != nil {
		log.Printf("Websocket received err: %s\n", err.Error())
		return
	}

	log.Printf("recv: %s %s\n", resp.Origin, resp.Command)

	switch resp.Origin {
	case "chia_full_node":
		m.serviceMetrics[chiaServiceFullNode].ReceiveResponse(resp)
	case "chia_wallet":
		m.serviceMetrics[chiaServiceWallet].ReceiveResponse(resp)
	case "chia_crawler":
		m.serviceMetrics[chiaServiceCrawler].ReceiveResponse(resp)
	case "chia_timelord":
		m.serviceMetrics[chiaServiceTimelord].ReceiveResponse(resp)
	}
}

func (m *Metrics) disconnectHandler() {
	for _, service := range m.serviceMetrics {
		service.Disconnected()
	}
}

// Healthcheck endpoint for metrics server
func healthcheckEndpoint(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, err := fmt.Fprintf(w, "Ok")
	if err != nil {
		log.Printf("Error writing healthcheck response %s\n", err.Error())
	}
}
