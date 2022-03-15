package metrics

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/chia-network/go-chia-libs/pkg/rpc"
	"github.com/chia-network/go-chia-libs/pkg/types"
	"github.com/prometheus/client_golang/prometheus"

	wrappedPrometheus "github.com/chia-network/chia-exporter/internal/prometheus"
	"github.com/chia-network/chia-exporter/internal/utils"
)

// Metrics that are based on Wallet RPC calls are in this file

// WalletServiceMetrics contains all metrics related to the wallet
type WalletServiceMetrics struct {
	// Holds a reference to the main metrics container this is a part of
	metrics *Metrics

	// WalletBalanceMetrics
	walletSynced            *wrappedPrometheus.LazyGauge
	confirmedBalance        *prometheus.GaugeVec
	spendableBalance        *prometheus.GaugeVec
	maxSendAmount           *prometheus.GaugeVec
	pendingCoinRemovalCount *prometheus.GaugeVec
	unspentCoinCount        *prometheus.GaugeVec
}

// InitMetrics sets all the metrics properties
func (s *WalletServiceMetrics) InitMetrics() {
	// Wallet Metrics
	s.walletSynced = s.metrics.newGauge(chiaServiceWallet, "synced", "")
	walletLabels := []string{"fingerprint", "wallet_id"}
	s.confirmedBalance = s.metrics.newGaugeVec(chiaServiceWallet, "confirmed_balance", "", walletLabels)
	s.spendableBalance = s.metrics.newGaugeVec(chiaServiceWallet, "spendable_balance", "", walletLabels)
	s.maxSendAmount = s.metrics.newGaugeVec(chiaServiceWallet, "max_send_amount", "", walletLabels)
	s.pendingCoinRemovalCount = s.metrics.newGaugeVec(chiaServiceWallet, "pending_coin_removal_count", "", walletLabels)
	s.unspentCoinCount = s.metrics.newGaugeVec(chiaServiceWallet, "unspent_coin_count", "", walletLabels)
}

// InitialData is called on startup of the metrics server, to allow seeding metrics with
// current/initial data
func (s *WalletServiceMetrics) InitialData() {
	// For bootstrapping the wallet, we're assuming wallet_id 1 for now
	// @TODO should actually get the IDs that exist, and ask for those instead
	// Better than (or in addition to) wallet ID is probably the asset ID for the wallet, particularly if its a CAT
	// Otherwise some other consistent identifier would be very useful for historical metrics across different nodes
	utils.LogErr(s.metrics.client.WalletService.GetWalletBalance(&rpc.GetWalletBalanceOptions{WalletID: 1}))
	utils.LogErr(s.metrics.client.WalletService.GetSyncStatus())
}

func (s *WalletServiceMetrics) Disconnected() {
	s.walletSynced.Unregister()
	s.confirmedBalance.Reset()
	s.spendableBalance.Reset()
	s.maxSendAmount.Reset()
	s.pendingCoinRemovalCount.Reset()
	s.unspentCoinCount.Reset()
}

// ReceiveResponse handles wallet responses that are returned over the websocket
func (s *WalletServiceMetrics) ReceiveResponse(resp *types.WebsocketResponse) {
	switch resp.Command {
	case "coin_added":
		s.CoinAdded(resp)
	case "sync_changed":
		s.SyncChanged(resp)
	case "get_sync_status":
		s.GetSyncStatus(resp)
	case "get_wallet_balance":
		s.GetWalletBalance(resp)
	}
}

// CoinAdded handles coin_added events by asking for wallet balance details
func (s *WalletServiceMetrics) CoinAdded(resp *types.WebsocketResponse) {
	coinAdded := &types.CoinAddedEvent{}
	err := json.Unmarshal(resp.Data, coinAdded)
	if err != nil {
		log.Printf("Error unmarshalling: %s\n", err.Error())
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
		log.Printf("Error unmarshalling: %s\n", err.Error())
		return
	}

	if syncStatusResponse.Synced {
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
		log.Printf("Error unmarshalling: %s\n", err.Error())
		return
	}

	if walletBalance.Balance != nil {
		fingerprint := fmt.Sprintf("%d", walletBalance.Balance.Fingerprint)
		walletID := fmt.Sprintf("%d", walletBalance.Balance.WalletID)

		if walletBalance.Balance.ConfirmedWalletBalance.FitsInUint64() {
			s.confirmedBalance.WithLabelValues(fingerprint, walletID).Set(float64(walletBalance.Balance.ConfirmedWalletBalance.Uint64()))
		}

		if walletBalance.Balance.SpendableBalance.FitsInUint64() {
			s.spendableBalance.WithLabelValues(fingerprint, walletID).Set(float64(walletBalance.Balance.SpendableBalance.Uint64()))
		}

		s.maxSendAmount.WithLabelValues(fingerprint, walletID).Set(float64(walletBalance.Balance.MaxSendAmount))
		s.pendingCoinRemovalCount.WithLabelValues(fingerprint, walletID).Set(float64(walletBalance.Balance.PendingCoinRemovalCount))
		s.unspentCoinCount.WithLabelValues(fingerprint, walletID).Set(float64(walletBalance.Balance.UnspentCoinCount))
	}
}
