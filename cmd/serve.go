package cmd

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/chia-network/chia-exporter/internal/metrics"
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Starts the metrics server",
	Run: func(cmd *cobra.Command, args []string) {
		level, err := log.ParseLevel(viper.GetString("log-level"))
		if err != nil {
			log.Fatalf("Error parsing log level: %s\n", err.Error())
		}
		m, err := metrics.NewMetrics(uint16(viper.GetInt("metrics-port")), level)
		if err != nil {
			log.Fatalln(err.Error())
		}

		// Run this in the background, so the metrics healthz endpoint can come up while waiting for Chia
		go startWebsocket(m)

		// Close the websocket when the app is closing
		// @TODO need to actually listen for a signal and call this then, otherwise it doesn't actually get called
		defer func(m *metrics.Metrics) {
			log.Println("App is stopping. Cleaning up...")
			err := m.CloseWebsocket()
			if err != nil {
				log.Errorf("Error closing websocket connection: %s\n", err.Error())
			}
		}(m)

		log.Fatalln(m.StartServer())
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

func startWebsocket(m *metrics.Metrics) {
	// Loop until we get a connection or cancel
	// This enables starting the metrics exporter even if the chia RPC service is not up/responding
	// It just retries every 5 seconds to connect to the RPC server until it succeeds or the app is stopped
	for {
		err := m.OpenWebsocket()
		if err != nil {
			log.Errorln(err.Error())
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}
}
