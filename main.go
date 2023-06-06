package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/chia-network/go-chia-libs/pkg/rpc"

	"github.com/chia-network/chia-exporter/internal/metrics"
)

func main() {
	ms, _ := metrics.NewMetrics(9914)

	ips := &rpc.GetIPsAfterTimestampResponse{}

	dat, _ := os.ReadFile("ips.json")
	err := json.Unmarshal(dat, ips)
	if err != nil {
		log.Fatalln(err)
	}
	ms.CrawlerServiceMetrics.ProcessIPASNMapping(ips)

	//cmd.Execute()
}
