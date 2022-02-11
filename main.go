package main

import (
	"log"
	"time"

	"github.com/chia-network/chia-exporter/internal/metrics"
)

func main() {
	// @TODO support setting alternate metrics port with config/env
	m, err := metrics.NewMetrics(9914)
	if err != nil {
		log.Fatalln(err.Error())
	}

	// Loop until we get a connection or cancel
	// This enables starting the metrics exporter even if the chia RPC service is not up/responding
	// It just retries every 5 seconds to connect to the RPC server until it succeeds or the app is stopped
	for {
		err = m.OpenWebsocket()
		if err != nil {
			log.Println(err.Error())
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}

	// Close the websocket when the app is closing
	// @TODO need to actually listen for a signal and call this then, otherwise it doesn't actually get called
	defer func(m *metrics.Metrics) {
		log.Println("App is stopping. Cleaning up...")
		err := m.CloseWebsocket()
		if err != nil {
			log.Printf("Error closing websocket connection: %s\n", err.Error())
		}
	}(m)

	log.Fatalln(m.StartServer())
}
