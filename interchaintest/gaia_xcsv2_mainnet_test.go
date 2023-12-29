package interchaintest_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/KyleMoser/Cronmos/helpers"
	"github.com/KyleMoser/Cronmos/logging"
	"github.com/KyleMoser/Cronmos/wasm"
	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	registry "github.com/KyleMoser/cosmos-client/client"
	"github.com/KyleMoser/cosmos-client/client/query"
	"github.com/KyleMoser/cosmos-client/cmd"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"github.com/stretchr/testify/suite"
	tsuite "github.com/stretchr/testify/suite"
	"go.uber.org/zap"
)

var (
	denomUosmo = "uosmo"
	denomUatom = "uatom"

	// The Osmosis crosschain swaps v2 contract. See github.com/osmosis-labs/osmosis/tree/main/cosmwasm/contracts/crosschain-swaps
	osmosisSwapForwardContract = "osmo1uwk8xc6q0s6t5qcpr6rht3sczu6du83xq8pwxjua0hfj5hzcnh3sqxwvxs"
	amountUosmoStr, _          = math.NewIntFromString("25000")
	amountUatomStr, _          = math.NewIntFromString("2000")
)

type GaiaMainnetTestSuite struct {
	suite.Suite
	*GaiaCommonTest
	osmosisConfig *OsmosisCommonTest
}

type GaiaCommonTest struct {
	logger                     *zap.Logger
	ctx                        context.Context
	cosmosHubHomePath          string
	cosmosHubTxSignerAddress   string
	txSignerOsmosisAddress     string
	osmoOnGaia                 string
	chainClientConfigCosmosHub *cosmosclient.ChainClientConfig
	cosmosHubChainClient       *cosmosclient.ChainClient
	cosmosHubTxSigner          helpers.CosmosUser
}

// Gaia mainnet test configuration is used in both our Gaia & Osmosis test cases.
// Note that you cannot recursively call testify's 'SetupTest' hence this intermediate.
func DoSetupTestGaia(suite *GaiaCommonTest, testifySuite *tsuite.Suite) {
	var err error
	sdk.SetAddrCacheEnabled(false)

	// Get the chain RPC URI from the Cosmos mainnet chain registry
	suite.chainClientConfigCosmosHub, err = registry.GetChain(context.Background(), chainRegCosmosHub, suite.logger)
	testifySuite.Require().NoError(err)

	// key used to submit TXs on Gaia mainnet
	cosmosHubKey, ok := os.LookupEnv("COSMOSHUB_KEY")
	if !ok {
		suite.logger.Warn("COSMOSHUB_KEY env not set", zap.String("Using default key", "default"))
		cosmosHubKey = "default"
	}

	// local home directory for Gaia
	cosmosHubHome, ok := os.LookupEnv("COSMOSHUB_HOME")
	if !ok {
		cosmosHubHome, err = os.UserHomeDir()
		if err != nil {
			panic("Set COSMOSHUB_HOME env to gaia home directory")
		}
		cosmosHubHome = filepath.Join(cosmosHubHome, ".gaia")
		suite.logger.Warn("COSMOSHUB_HOME env not set", zap.String("Using default home directory", cosmosHubHome))
	}

	suite.cosmosHubHomePath = cosmosHubHome
	suite.chainClientConfigCosmosHub.Modules = cmd.ModuleBasics
	suite.chainClientConfigCosmosHub.Key = cosmosHubKey
	suite.cosmosHubChainClient, err = cosmosclient.NewChainClient(suite.logger, suite.chainClientConfigCosmosHub, suite.cosmosHubHomePath, nil, nil)
	testifySuite.Require().NoError(err)

	// Ensure the SDK bech32 prefixes are set to "cosmos"
	sdkCtxMu := suite.cosmosHubChainClient.SetSDKContext()
	defer sdkCtxMu()

	// Get the Gaia and Osmosis addresses corresponding to the configured key
	defaultAcc, err := suite.cosmosHubChainClient.GetKeyAddress()
	testifySuite.Require().NoError(err)
	suite.cosmosHubTxSigner = helpers.CosmosUser{Address: defaultAcc, FromName: suite.cosmosHubChainClient.Config.Key}
	suite.cosmosHubTxSignerAddress = suite.cosmosHubTxSigner.ToBech32(suite.chainClientConfigCosmosHub.AccountPrefix)
	suite.txSignerOsmosisAddress = suite.cosmosHubTxSigner.ToBech32("osmo")

	// Trace IBC Denom (OSMO on CosmosHub)
	osmoDenomTrace := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom("transfer", "channel-141", "uosmo"))
	suite.osmoOnGaia = osmoDenomTrace.IBCDenom()
}

// Set COSMOSHUB_KEY environment variable to a signing key. Signer must have a small amount of uatom plus TX fees.
// Set COSMOSHUB_HOME environment variable to Gaia home directory (or default will automatically be chosen).
func (suite *GaiaMainnetTestSuite) SetupTest() {
	suite.GaiaCommonTest = &GaiaCommonTest{}
	suite.logger = logging.DoConfigureLogger("DEBUG")
	suite.ctx = context.Background()
	DoSetupTestGaia(suite.GaiaCommonTest, &suite.Suite)

	suite.osmosisConfig = &OsmosisCommonTest{
		logger: logging.DoConfigureLogger("DEBUG"),
		ctx:    context.Background(),
	}

	DoSetupTestOsmosis(suite.osmosisConfig, &suite.Suite)
}

func TestMainnetTestSuite(t *testing.T) {
	suite.Run(t, new(GaiaMainnetTestSuite))
}

// Performs an IBC transfer with a memo from CosmosHub, which invokes the SwapRouter contract on Osmosis.
// Funds are swapped from ATOM to OSMO, then OSMO is left on Osmosis.
func (suite *GaiaMainnetTestSuite) TestSwapAndLeave() {
	// Ensure the SDK bech32 prefixes are set to "cosmos"
	sdkCtxMu := suite.cosmosHubChainClient.SetSDKContext()
	defer sdkCtxMu()

	// From TestGetMainnetXcsv2Config in mainnet_routes_test.go
	swapRouterContractAddr := "osmo1fy547nr4ewfc38z73ghr6x62p7eguuupm66xwk8v8rjnjyeyxdqs6gdqx7"

	cosmosHubHeight, err := suite.cosmosHubChainClient.QueryLatestHeight(suite.ctx)
	suite.Require().NoError(err)

	// Check the current balance of OSMO on Osmosis
	query := query.Query{Client: suite.osmosisConfig.osmosisChainClient, Options: &query.QueryOptions{}}
	initialBalanceOsmoOsmosis, err := query.Bank_Balance(suite.txSignerOsmosisAddress, denomUosmo)
	suite.Require().NoError(err)
	suite.logger.Debug("OSMO on Osmosis (initial balance)", zap.String("Coin", initialBalanceOsmoOsmosis.Balance.String()))

	// Create an IBC hooks memo that will invoke the Osmosis crosschain swap (XCSv2) contract
	swapRouterWasmMemo, err := wasm.SwapRouterMemo(wasm.Coin{
		Denom:  suite.osmosisConfig.atomOnOsmosis,
		Amount: amountUatomStr.String(),
	}, denomUosmo, swapRouterContractAddr)
	suite.Require().NoError(err)

	// Create IBC Transfer message with channel, port, and client ID from IBC chain registry.
	// See https://github.com/cosmos/chain-registry/blob/master/_IBC/cosmoshub-osmosis.json.
	msgTransfer, err := helpers.PrepareIbcTransfer(suite.cosmosHubChainClient, cosmosHubHeight, "channel-141", "transfer", "07-tendermint-259")
	suite.Require().NoError(err)

	msgTransfer.Sender = suite.cosmosHubTxSignerAddress
	msgTransfer.Receiver = swapRouterContractAddr
	msgTransfer.Token = sdk.NewCoin(denomUatom, amountUatomStr)
	msgTransfer.Memo = swapRouterWasmMemo // this will invoke IBC hooks on the recipient chain

	broadcaster := cosmosclient.NewBroadcaster(suite.cosmosHubChainClient)

	// Crosschain swaps use about this much gas
	broadcaster.ConfigureFactoryOptions(func(factory tx.Factory) tx.Factory {
		factory = factory.WithGas(150000)
		return factory
	})

	gaiaHeightPreXcs, err := suite.cosmosHubChainClient.QueryLatestHeight(suite.ctx)
	suite.Require().NoError(err)
	desiredHeight := gaiaHeightPreXcs + 15

	resp, err := cosmosclient.BroadcastTx(suite.ctx, broadcaster, &suite.cosmosHubTxSigner, msgTransfer)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), resp)

	// Wait for 15 blocks (relaying XCS can be slow on mainnet)
	err = testutil.WaitForCondition(time.Second*120, time.Second*5, func() (bool, error) {
		gaiaHeight, err := suite.cosmosHubChainClient.QueryLatestHeight(suite.ctx)
		if err != nil {
			return false, nil
		}
		return gaiaHeight >= desiredHeight, nil
	})
	suite.Require().NoError(err)

	// Check the final balance of OSMO on Osmosis
	finalBalanceOsmoOsmosis, err := query.Bank_Balance(suite.txSignerOsmosisAddress, denomUosmo)
	suite.Require().NoError(err)
	suite.logger.Debug("OSMO on CosmosHub (final balance)", zap.String("Coin", finalBalanceOsmoOsmosis.Balance.String()))
	suite.Require().True(finalBalanceOsmoOsmosis.Balance.Amount.GT(initialBalanceOsmoOsmosis.Balance.Amount))
}

// Performs an IBC transfer with a memo from CosmosHub, which invokes the XCSv2 contract on Osmosis.
// Funds are swapped from ATOM to OSMO, then OSMO is IBC transferred back to Cosmoshub automatically.
// See SetupTest() for configuration requirements.
func (suite *GaiaMainnetTestSuite) TestXCSv2Memo() {
	// Ensure the SDK bech32 prefixes are set to "cosmos"
	sdkCtxMu := suite.cosmosHubChainClient.SetSDKContext()
	defer sdkCtxMu()

	cosmosHubHeight, err := suite.cosmosHubChainClient.QueryLatestHeight(suite.ctx)
	suite.Require().NoError(err)

	// Check the current balance of OSMO on CosmosHub
	query := query.Query{Client: suite.cosmosHubChainClient, Options: &query.QueryOptions{}}
	initialBalanceOsmoGaia, err := query.Bank_Balance(suite.cosmosHubTxSignerAddress, suite.osmoOnGaia)
	suite.Require().NoError(err)
	suite.logger.Debug("OSMO on CosmosHub (initial balance)", zap.String("Coin", initialBalanceOsmoGaia.Balance.String()))

	// Create an IBC hooks memo that will invoke the Osmosis crosschain swap (XCVv2) contract
	xcsWasmMemo, err := wasm.CrosschainSwapMemo(suite.txSignerOsmosisAddress, suite.cosmosHubTxSignerAddress, denomUosmo, osmosisSwapForwardContract)
	suite.Require().NoError(err)

	// Create IBC Transfer message with channel, port, and client ID from IBC chain registry.
	// See https://github.com/cosmos/chain-registry/blob/master/_IBC/cosmoshub-osmosis.json.
	msgTransfer, err := helpers.PrepareIbcTransfer(suite.cosmosHubChainClient, cosmosHubHeight, "channel-141", "transfer", "07-tendermint-259")
	suite.Require().NoError(err)

	msgTransfer.Sender = suite.cosmosHubTxSignerAddress
	msgTransfer.Receiver = osmosisSwapForwardContract
	msgTransfer.Token = sdk.NewCoin(denomUatom, amountUatomStr)
	msgTransfer.Memo = xcsWasmMemo // this will invoke IBC hooks on the recipient chain

	broadcaster := cosmosclient.NewBroadcaster(suite.cosmosHubChainClient)

	// Crosschain swaps use about this much gas
	broadcaster.ConfigureFactoryOptions(func(factory tx.Factory) tx.Factory {
		factory = factory.WithGas(300000)
		return factory
	})

	gaiaHeightPreXcs, err := suite.cosmosHubChainClient.QueryLatestHeight(suite.ctx)
	suite.Require().NoError(err)
	desiredHeight := gaiaHeightPreXcs + 15

	resp, err := cosmosclient.BroadcastTx(suite.ctx, broadcaster, &suite.cosmosHubTxSigner, msgTransfer)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), resp)

	// Wait for 15 blocks (relaying XCS can be slow on mainnet)
	err = testutil.WaitForCondition(time.Second*120, time.Second*5, func() (bool, error) {
		gaiaHeight, err := suite.cosmosHubChainClient.QueryLatestHeight(suite.ctx)
		if err != nil {
			return false, nil
		}
		return gaiaHeight >= desiredHeight, nil
	})
	suite.Require().NoError(err)

	// Check the final balance of OSMO on CosmosHub
	finalBalanceOsmoGaia, err := query.Bank_Balance(suite.cosmosHubTxSignerAddress, suite.osmoOnGaia)
	suite.Require().NoError(err)
	suite.logger.Debug("OSMO on CosmosHub (final balance)", zap.String("Coin", finalBalanceOsmoGaia.Balance.String()))
	suite.Require().True(finalBalanceOsmoGaia.Balance.Amount.GT(initialBalanceOsmoGaia.Balance.Amount))
}
