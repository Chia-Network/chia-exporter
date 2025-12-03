package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/chia-network/go-chia-libs/pkg/rpc"
	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"

	wrappedPrometheus "github.com/chia-network/go-modules/pkg/prometheus"

	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Wallet RPC calls are in this file

// WalletServiceMetrics contains all metrics related to the wallet
type WalletServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// General Service Metrics
	gotVersionResponse bool
	version            *prometheus.GaugeVec

	gotWalletsResponse bool

	// Connection Metrics
	connectionCount *prometheus.GaugeVec

	// WalletBalanceMetrics
	walletSynced            *wrappedPrometheus.LazyGauge
	confirmedBalance        *prometheus.GaugeVec
	spendableBalance        *prometheus.GaugeVec
	maxSendAmount           *prometheus.GaugeVec
	pendingCoinRemovalCount *prometheus.GaugeVec
	unspentCoinCount        *prometheus.GaugeVec

	// Debug Metric
	debug *prometheus.GaugeVec
}

// InitMetrics sets all the metrics properties
func (s *WalletServiceMetrics) InitMetrics(network *string) {
	// General Service Metrics
	s.version = s.metrics.newGaugeVec(chiaServiceWallet, "version", "The version of chia-blockchain the service is running", []string{"version"})

	// Connection Metrics
	s.connectionCount = s.metrics.newGaugeVec(chiaServiceWallet, "connection_count", "Number of active connections for each type of peer", []string{"node_type"})

	// Wallet Metrics
	s.walletSynced = s.metrics.newGauge(chiaServiceWallet, "synced", "")
	walletLabels := []string{"fingerprint", "wallet_id", "wallet_type", "asset_id"}
	s.confirmedBalance = s.metrics.newGaugeVec(chiaServiceWallet, "confirmed_balance", "", walletLabels)
	s.spendableBalance = s.metrics.newGaugeVec(chiaServiceWallet, "spendable_balance", "", walletLabels)
	s.maxSendAmount = s.metrics.newGaugeVec(chiaServiceWallet, "max_send_amount", "", walletLabels)
	s.pendingCoinRemovalCount = s.metrics.newGaugeVec(chiaServiceWallet, "pending_coin_removal_count", "", walletLabels)
	s.unspentCoinCount = s.metrics.newGaugeVec(chiaServiceWallet, "unspent_coin_count", "", walletLabels)

	// Debug Metric
	s.debug = s.metrics.newGaugeVec(chiaServiceWallet, "debug_metrics", "misc debugging metrics distinguished by labels", []string{"key"})
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with
// current/initial data
func (s *WalletServiceMetrics) InitialData() {
	// Only get the version on an initial or reconnection
	utils.LogErr(s.metrics.client.WalletService.GetVersion(&rpc.GetVersionOptions{}))

	utils.LogErr(s.metrics.client.WalletService.GetWallets(&rpc.GetWalletsOptions{}))
	utils.LogErr(s.metrics.client.WalletService.GetSyncStatus())
}

// SetupPollingMetrics starts any metrics that happen on an interval
func (s *WalletServiceMetrics) SetupPollingMetrics(ctx context.Context) {
	// Things that update in the background
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				// Exit the loop if the context is canceled
				return
			default:
				utils.LogErr(s.metrics.client.WalletService.GetConnections(&rpc.GetConnectionsOptions{}))
				time.Sleep(15 * time.Second)
			}
		}
	}(ctx)
}

// Disconnected clears/unregisters metrics when the connection drops
func (s *WalletServiceMetrics) Disconnected() {
	s.version.Reset()
	s.gotVersionResponse = false
	s.gotWalletsResponse = false
	s.connectionCount.Reset()
	s.walletSynced.Unregister()
	s.confirmedBalance.Reset()
	s.spendableBalance.Reset()
	s.maxSendAmount.Reset()
	s.pendingCoinRemovalCount.Reset()
	s.unspentCoinCount.Reset()
}

// Reconnected is called when the service is reconnected after the websocket was disconnected
func (s *WalletServiceMetrics) Reconnected() {
	s.InitialData()
}

// ReceiveResponse handles wallet responses that are returned over the websocket
func (s *WalletServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	// Sometimes, when we reconnect, or start exporter before chia is running
	// the daemon is up before the service, and the initial request for the version
	// doesn't make it to the service
	// daemon doesn't queue these messages for later, they just get dropped
	if !s.gotVersionResponse {
		utils.LogErr(s.metrics.client.FullNodeService.GetVersion(&rpc.GetVersionOptions{}))
	}
	if !s.gotWalletsResponse {
		utils.LogErr(s.metrics.client.WalletService.GetWallets(&rpc.GetWalletsOptions{}))
	}

	switch resp.Command {
	case "get_version":
		versionHelper(resp, s.version)
		s.gotVersionResponse = true
	case "get_connections":
		s.GetConnections(resp)
	case "coin_added":
		s.CoinAdded(resp)
	case "sync_changed":
		s.SyncChanged(resp)
	case "get_sync_status":
		s.GetSyncStatus(resp)
	case "get_wallet_balance":
		s.GetWalletBalance(resp)
	case "get_wallets":
		s.GetWallets(resp)
		s.gotWalletsResponse = true
	case "debug":
		debugHelper(resp, s.debug)
	}
}

// GetConnections handler for get_connections events
func (s *WalletServiceMetrics) GetConnections(resp *types.WebsocketResponse) {
	connectionCountHelper(resp, s.connectionCount)
}

// CoinAdded handles coin_added events by asking for wallet balance details
func (s *WalletServiceMetrics) CoinAdded(resp *types.WebsocketResponse) {
	coinAdded := &types.CoinAddedEvent{}
	err := json.Unmarshal(resp.Data, coinAdded)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	utils.LogErr(s.metrics.client.WalletService.GetWalletBalance(&rpc.GetWalletBalanceOptions{WalletID: coinAdded.WalletID}))
	utils.LogErr(s.metrics.client.WalletService.GetSyncStatus())
}

// SyncChanged handles the sync_changed event from the websocket
func (s *WalletServiceMetrics) SyncChanged(resp *types.WebsocketResponse) {
	// @TODO probably should throttle this call in case we're in a longer sync
	utils.LogErr(s.metrics.client.WalletService.GetSyncStatus())
}

// GetSyncStatus sync status for the wallet
func (s *WalletServiceMetrics) GetSyncStatus(resp *types.WebsocketResponse) {
	syncStatusResponse := &rpc.GetWalletSyncStatusResponse{}
	err := json.Unmarshal(resp.Data, syncStatusResponse)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	if syncStatusResponse.Synced.OrEmpty() {
		s.walletSynced.Set(1)
	} else {
		s.walletSynced.Set(0)
	}
}

// GetWalletBalance updates wallet balance metrics in response to balance changes
func (s *WalletServiceMetrics) GetWalletBalance(resp *types.WebsocketResponse) {
	walletBalance := &rpc.GetWalletBalanceResponse{}
	err := json.Unmarshal(resp.Data, walletBalance)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	if walletBal, hasWalletBal := walletBalance.Balance.Get(); hasWalletBal {
		fingerprint := fmt.Sprintf("%d", walletBal.Fingerprint)
		walletID := fmt.Sprintf("%d", walletBal.WalletID)
		walletType := fmt.Sprintf("%d", walletBal.WalletType)
		assetID := walletBal.AssetID

		if walletBal.ConfirmedWalletBalance.FitsInUint64() {
			s.confirmedBalance.WithLabelValues(fingerprint, walletID, walletType, assetID).Set(float64(walletBal.ConfirmedWalletBalance.Uint64()))
		}

		if walletBal.SpendableBalance.FitsInUint64() {
			s.spendableBalance.WithLabelValues(fingerprint, walletID, walletType, assetID).Set(float64(walletBal.SpendableBalance.Uint64()))
		}

		if walletBal.MaxSendAmount.FitsInUint64() {
			s.maxSendAmount.WithLabelValues(fingerprint, walletID, walletType, assetID).Set(float64(walletBal.MaxSendAmount.Uint64()))
		}
		s.pendingCoinRemovalCount.WithLabelValues(fingerprint, walletID, walletType, assetID).Set(float64(walletBal.PendingCoinRemovalCount))
		s.unspentCoinCount.WithLabelValues(fingerprint, walletID, walletType, assetID).Set(float64(walletBal.UnspentCoinCount))
	}
}

// GetWallets handles a response for get_wallets and asks for the balance of each wallet
func (s *WalletServiceMetrics) GetWallets(resp *types.WebsocketResponse) {
	wallets := &rpc.GetWalletsResponse{}
	err := json.Unmarshal(resp.Data, wallets)
	if err != nil {
		log.Errorf("Error unmarshalling: %s\n", err.Error())
		return
	}

	for _, wallet := range wallets.Wallets.OrEmpty() {
		utils.LogErr(s.metrics.client.WalletService.GetWalletBalance(&rpc.GetWalletBalanceOptions{WalletID: wallet.ID}))
	}
}
