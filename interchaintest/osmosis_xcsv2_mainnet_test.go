package interchaintest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/KyleMoser/Cronmos/helpers"
	"github.com/KyleMoser/Cronmos/logging"
	"github.com/KyleMoser/Cronmos/wasm"
	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	registry "github.com/KyleMoser/cosmos-client/client/chain_registry"
	"github.com/KyleMoser/cosmos-client/client/query"
	"github.com/KyleMoser/cosmos-client/cmd"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
)

type OsmosisMainnetTestSuite struct {
	suite.Suite
	gaiaConfig               *GaiaCommonTest
	osmosisHomePath          string
	osmosisTxSignerAddress   string
	atomOnOsmosis            string
	chainClientConfigOsmosis *cosmosclient.ChainClientConfig
	osmosisChainClient       *cosmosclient.ChainClient
	osmosisTxSigner          helpers.CosmosUser
}

func TestOsmosisMainnetTestSuite(t *testing.T) {
	suite.Run(t, new(OsmosisMainnetTestSuite))
}

func (suite *OsmosisMainnetTestSuite) SetupTest() {
	var err error
	sdk.SetAddrCacheEnabled(false)
	suite.gaiaConfig = &GaiaCommonTest{
		logger: logging.DoConfigureLogger("DEBUG"),
		ctx:    context.Background(),
	}
	suite.atomOnOsmosis = "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2"

	// Get the chain RPC URI from the Osmosis mainnet chain registry
	suite.chainClientConfigOsmosis, err = registry.GetChain(context.Background(), chainRegOsmosis, suite.gaiaConfig.logger)
	suite.Require().NoError(err)

	// key used to submit TXs on Osmosis mainnet
	osmosisKey, ok := os.LookupEnv("OSMOSIS_KEY")
	if !ok {
		suite.gaiaConfig.logger.Warn("OSMOSIS_KEY env not set", zap.String("Using default key", "default"))
		osmosisKey = "default"
	}

	// local home directory for Osmosis
	osmosisHome, ok := os.LookupEnv("OSMOSIS_HOME")
	if !ok {
		osmosisHome, err = os.UserHomeDir()
		if err != nil {
			panic("Set OSMOSIS_HOME env to Osmosis home directory")
		}
		osmosisHome = filepath.Join(osmosisHome, ".osmosisd")
		suite.gaiaConfig.logger.Warn("OSMOSIS_HOME env not set", zap.String("Using default home directory", osmosisHome))
	}

	suite.osmosisHomePath = osmosisHome
	suite.chainClientConfigOsmosis.Modules = cmd.ModuleBasics
	suite.chainClientConfigOsmosis.Key = osmosisKey
	suite.osmosisChainClient, err = cosmosclient.NewChainClient(suite.gaiaConfig.logger, suite.chainClientConfigOsmosis, suite.osmosisHomePath, nil, nil)
	suite.Require().NoError(err)

	// Ensure the SDK bech32 prefixes are set to "osmo"
	sdkCtxMu := suite.osmosisChainClient.SetSDKContext()

	// Get the Osmosis addresses corresponding to the configured key
	defaultAcc, err := suite.osmosisChainClient.GetKeyAddress()
	suite.Require().NoError(err)
	suite.osmosisTxSigner = helpers.CosmosUser{Address: defaultAcc, FromName: suite.osmosisChainClient.Config.Key}
	suite.osmosisTxSignerAddress = suite.osmosisTxSigner.ToBech32(suite.chainClientConfigOsmosis.AccountPrefix)
	sdkCtxMu() // release bech32 mutex lock

	// To check balances we will need the chain client from Gaia.
	DoSetupTestGaia(suite.gaiaConfig, &suite.Suite)
}

// This performs a crosschain swap ON osmosis (the XCSv2 contract is called directly on osmosis). The funds are transferred via IBC to Cosmoshub.
func (suite *OsmosisMainnetTestSuite) TestSubmitTx() {
	// Initial height of CosmosHub (before sending XCSv2 swap)
	sdkCtxMu := suite.gaiaConfig.cosmosHubChainClient.SetSDKContext()
	cosmosHubInitialHeight, err := suite.gaiaConfig.cosmosHubChainClient.QueryLatestHeight(suite.gaiaConfig.ctx)
	suite.Require().NoError(err)
	desiredHeight := cosmosHubInitialHeight + 15

	// Check the current balance of ATOM on CosmosHub
	query := query.Query{Client: suite.gaiaConfig.cosmosHubChainClient, Options: &query.QueryOptions{}}
	initialBalanceAtomGaia, err := query.Bank_Balance(suite.gaiaConfig.cosmosHubTxSignerAddress, "uatom")
	suite.Require().NoError(err)
	suite.gaiaConfig.logger.Debug("ATOM on CosmosHub (initial balance)", zap.String("Coin", initialBalanceAtomGaia.Balance.String()))
	sdkCtxMu()

	msg := wasm.ExecuteMsg{
		Swap: &wasm.CrosschainSwap{
			OutputDenom: suite.atomOnOsmosis,
			Receiver:    suite.gaiaConfig.cosmosHubTxSignerAddress,
			Slippage: wasm.Slippage{
				Twap: wasm.Twap{
					Percentage: "5",
					Window:     10,
				},
			},
			OnFailedDelivery: wasm.OnFailedDelivery{
				RecoveryAddress: suite.gaiaConfig.txSignerOsmosisAddress,
			},
		},
	}

	b, _ := json.Marshal(msg)
	fmt.Println(string(b))

	req := &types.MsgExecuteContract{
		Sender:   suite.gaiaConfig.txSignerOsmosisAddress,
		Contract: osmosisSwapForwardContract,
		Msg:      b,
		Funds: []sdk.Coin{
			sdk.NewCoin("uosmo", amountStr),
		},
	}

	ctx := context.Background()
	broadcaster := cosmosclient.NewBroadcaster(suite.osmosisChainClient)

	// Crosschain swaps use about this much gas
	broadcaster.ConfigureFactoryOptions(func(factory tx.Factory) tx.Factory {
		factory = factory.WithGas(700000)
		return factory
	})

	sdkCtxMu = suite.osmosisChainClient.SetSDKContext()
	resp, err := cosmosclient.BroadcastTx(ctx, broadcaster, &suite.osmosisTxSigner, req)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), resp)
	sdkCtxMu()

	// Wait for 15 blocks (relaying XCS can be incredibly slow on mainnet)
	err = testutil.WaitForCondition(time.Second*120, time.Second*5, func() (bool, error) {
		gaiaHeight, err := suite.gaiaConfig.cosmosHubChainClient.QueryLatestHeight(suite.gaiaConfig.ctx)
		if err != nil {
			return false, nil
		}
		return gaiaHeight >= desiredHeight, nil
	})
	suite.Require().NoError(err)

	// Check the final balance of ATOM on CosmosHub
	finalBalanceAtomGaia, err := query.Bank_Balance(suite.gaiaConfig.cosmosHubTxSignerAddress, "uatom")
	suite.Require().NoError(err)
	suite.gaiaConfig.logger.Debug("ATOM on CosmosHub (final balance)", zap.String("Coin", finalBalanceAtomGaia.Balance.String()))
	suite.Require().True(finalBalanceAtomGaia.Balance.Amount.GT(initialBalanceAtomGaia.Balance.Amount))
}
