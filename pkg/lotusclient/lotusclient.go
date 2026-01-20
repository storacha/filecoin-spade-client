package lotusclient

import (
	"context"
	"encoding/base64"
	"filecoin-spade-client/pkg/config"
	"filecoin-spade-client/pkg/log"
	"fmt"
	"net/http"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/abi"
	lotusapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
)

type LotusClient struct {
	Config   config.LotusConfig
	Api      lotusapi.FullNodeStruct
	MinerApi lotusapi.StorageMinerStruct

	MinerAddress  address.Address
	WorkerAddress address.Address
}

func New(config config.Configuration) *LotusClient {
	lc := new(LotusClient)
	lc.Config = config.LotusConfig

	return lc
}

func (lc *LotusClient) Start(ctx context.Context) {
	daemonctx, cancelDaemon := context.WithCancel(ctx)
	lc.connectLotusDaemon(daemonctx)

	minerctx, cancelMiner := context.WithCancel(ctx)
	lc.connectLotusMiner(minerctx)

	go func() {
		select {
		case <-ctx.Done():
			log.Infof("shutting down main lotus client: context done")
			cancelDaemon()
			cancelMiner()
			return
		}
	}()
}

func (lc *LotusClient) connectLotusDaemon(ctx context.Context) {
	// Setup daemon API
	closer, err := jsonrpc.NewMergeClient(
		context.Background(),
		lc.Config.DaemonUrl,
		"Filecoin",
		[]interface{}{&lc.Api.Internal, &lc.Api.CommonStruct.Internal},
		http.Header{"Authorization": []string{"Bearer " + lc.Config.DaemonAuthToken}},
	)
	if err != nil {
		log.Fatalf("connecting with Lotus Daemon failed: %s", err)
	}

	// Check if we are in sync
	nodestatus, err := lc.Api.NodeStatus(ctx, false)
	if err != nil {
		log.Fatalf("error checking node status: %s", err)
	}

	// Expected mainnet epoch
	expectedEpoch := uint64((time.Now().Unix() - 1598306400) / 30)
	actualBehind := expectedEpoch - nodestatus.SyncStatus.Epoch

	if nodestatus.SyncStatus.Behind > 5 || actualBehind > 5 {
		log.Fatalf("daemon is not in sync: node reported behind %d, actual behind %d", nodestatus.SyncStatus.Behind, actualBehind)
	}

	log.Infof("Successfully connected to main lotus node, chain in sync")

	go func() {
		select {
		case <-ctx.Done():
			log.Infof("shutting down lotus daemon client: context done")
			closer()
			return
		}
	}()
}

func (lc *LotusClient) getCurrentEpoch(ctx context.Context) abi.ChainEpoch {
	// Check if we are in sync
	nodestatus, err := lc.Api.NodeStatus(ctx, false)
	if err != nil {
		log.Fatalf("error getting current epoch: %s", err)
	}
	return abi.ChainEpoch(nodestatus.SyncStatus.Epoch)
}

func (lc *LotusClient) getFinalizedTipset(ctx context.Context) *types.TipSet {
	tipset, err := lc.Api.ChainGetTipSetByHeight(ctx, lc.getCurrentEpoch(ctx)-900, types.TipSetKey{})
	if err != nil {
		log.Fatalf("error fetching finalized tipset: %s", err)
	}
	return tipset
}

func (lc *LotusClient) connectLotusMiner(ctx context.Context) {
	// Setup daemon API
	closer, err := jsonrpc.NewMergeClient(
		context.Background(),
		lc.Config.MinerUrl,
		"Filecoin",
		[]interface{}{&lc.MinerApi.Internal, &lc.MinerApi.CommonStruct.Internal},
		http.Header{"Authorization": []string{"Bearer " + lc.Config.MinerAuthToken}},
	)
	if err != nil {
		log.Fatalf("connecting with Lotus Miner failed: %s", err)
	}

	// Check our SP ID
	actorAddress, err := lc.MinerApi.ActorAddress(ctx)
	if err != nil {
		log.Fatalf("error checking actor address: %s", err)
	}

	lc.MinerAddress = actorAddress

	// Check our Worker ID
	minerInfo, err := lc.Api.StateMinerInfo(ctx, actorAddress, lc.getFinalizedTipset(ctx).Key())
	if err != nil {
		log.Fatalf("error checking miner info: %s", err)
	}

	lc.WorkerAddress = minerInfo.Worker

	log.Infof("Successfully connected to lotus miner node, Miner Address: %s, Worker %s", lc.MinerAddress, lc.WorkerAddress)

	go func() {
		select {
		case <-ctx.Done():
			log.Infof("shutting down lotus miner client: context done")
			closer()
			return
		}
	}()
}

func (lc *LotusClient) GetSpadeAuthSignature(ctx context.Context, authPrefix string) string {
	currentEpoch := lc.getCurrentEpoch(ctx)
	beaconEntry, err := lc.Api.StateGetBeaconEntry(ctx, currentEpoch)
	if err != nil {
		log.Fatalf("error getting beacon entry: %s", err)
	}

	// Prefix the beacon data with 3 spaces
	beaconData := append([]byte("   "), beaconEntry.Data...)

	base64OptionalPayload := ""
	if authPrefix != "" {
		base64OptionalPayload = base64.StdEncoding.EncodeToString([]byte(authPrefix))
	}
	beaconData = append(beaconData, []byte(authPrefix)...)

	// Try to sign
	walletSign, err := lc.Api.WalletSign(ctx, lc.WorkerAddress, beaconData)
	if err != nil {
		log.Fatalf("error signing with wallet: %s", err)
	}
	signature := fmt.Sprintf("%s %d;%s;%s", "FIL-SPID-V0", currentEpoch, lc.MinerAddress, base64.StdEncoding.EncodeToString(walletSign.Data))
	if base64OptionalPayload != "" {
		signature = fmt.Sprintf("%s;%s", signature, base64OptionalPayload)
	}
	return signature
}
