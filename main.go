package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/chia-network/go-chia-libs/pkg/rpc"

	"github.com/chia-network/chia-exporter/internal/metrics"
)

func main() {
	/*
	# On the crawler
	date -d "5 days ago" "+%s"
	chia rpc crawler get_ips_after_timestamp '{"after":1687710018,"limit":110476}' > ips.json

	# Download the json, put the file in the root of this project, then run:
	go run main.go
	*/

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
