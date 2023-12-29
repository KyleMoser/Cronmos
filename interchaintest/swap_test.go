package interchaintest_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/KyleMoser/Cronmos/helpers"
	"github.com/KyleMoser/Cronmos/logging"
	"github.com/KyleMoser/Cronmos/wasm"
	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	registry "github.com/KyleMoser/cosmos-client/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	ctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	disttypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/go-bip39"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"

	client "github.com/docker/docker/client"
	"github.com/strangelove-ventures/interchaintest/v8"
	"github.com/strangelove-ventures/interchaintest/v8/chain/cosmos"
	"github.com/strangelove-ventures/interchaintest/v8/ibc"
	"github.com/strangelove-ventures/interchaintest/v8/testreporter"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var (
	// Fetch chain info from chain registry. Must match the relevant directory name in the chain registry here:
	// https://github.com/cosmos/chain-registry
	chainRegCosmosHub  = "cosmoshub"
	ibcPath            = "gaia-osmosis"
	chainRegOsmosis    = "osmosis"
	withdrawCommission = "/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission"
	withdrawReward     = "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward"
)

func TestQueryBlockEvents(t *testing.T) {
	logger := logging.DoConfigureLogger("DEBUG")
	chainClientConfigOsmosis, err := registry.GetChain(context.Background(), chainRegOsmosis, logger)
	require.NoError(t, err)
	osmosisChainClient, err := cosmosclient.NewChainClient(logger, chainClientConfigOsmosis, "/home/kyle/.osmosis", nil, nil)
	require.NoError(t, err)
	height := int64(12693016)
	res, err := osmosisChainClient.RPCClient.BlockResults(context.Background(), &height)
	require.NoError(t, err)

	fmt.Printf("block events: %+v", res.FinalizeBlockEvents)

	for _, curr := range res.TxsResults {
		if curr.GasWanted == 845879 {
			data := string(curr.Data)
			fmt.Printf("Data: %s\n", data)
			for _, evt := range curr.Events {
				fmt.Printf("event type: %s\n", evt.Type)
				for _, attr := range evt.Attributes {
					k, _ := base64.StdEncoding.DecodeString(attr.Key)
					v, _ := base64.StdEncoding.DecodeString(attr.Value)

					fmt.Printf("attr key: %s, value: %s\n", k, v)
				}
			}
		}
	}
}

type SellRewardsTestSuite struct {
	suite.Suite
	logger *zap.Logger
	//gaiaChainClient          *cosmosclient.ChainClient
	osmosisChainClient    *cosmosclient.ChainClient
	osmosisTxSigner       helpers.CosmosUser
	osmosis               ibc.Chain
	gaia                  ibc.Chain
	r                     ibc.Relayer
	rep                   *testreporter.Reporter
	eRep                  *testreporter.RelayerExecReporter
	ic                    *interchaintest.Interchain
	client                *client.Client
	network               string
	gaiaValidatorMnemonic string
}

func getValidatorCommissionBalancesMap(
	distClient disttypes.QueryClient,
	bankClient banktypes.QueryClient,
	ctx context.Context,
	valoperBech32 string,
	valAddr sdk.AccAddress,
) (
	balancesBeforeClaim map[string]sdk.Coin,
	expectedCommisionMap map[string]sdkmath.Int,
	err error,
) {
	expectedCommisionMap = map[string]sdkmath.Int{}
	balancesBeforeClaim = map[string]sdk.Coin{}

	valCommissionResp, err := distClient.ValidatorCommission(
		ctx,
		&disttypes.QueryValidatorCommissionRequest{ValidatorAddress: valoperBech32},
	)
	if err != nil {
		return nil, nil, err
	}
	unclaimedCommission := valCommissionResp.GetCommission()
	unclaimedCommissionCoins := unclaimedCommission.GetCommission()

	// Check the current bank balance for the validator, for the denoms we can claim as commission
	for _, unclaimedCommissionCoin := range unclaimedCommissionCoins {
		queryBalReq := banktypes.NewQueryBalanceRequest(valAddr, unclaimedCommissionCoin.Denom)
		res, err := bankClient.Balance(ctx, queryBalReq)
		if err != nil {
			return nil, nil, err
		}
		balancesBeforeClaim[unclaimedCommissionCoin.Denom] = *res.Balance
		expectedCommisionMap[unclaimedCommissionCoin.Denom] = unclaimedCommissionCoin.Amount.TruncateInt()
	}

	return
}

// TestAuthzClaimRewards Authorize grantee ability to claim rewards, then execute the claim.
// Run this test with e.g. go test -timeout 300s -run ^TestAuthzClaimRewards$ github.com/KyleMoser/Cronmos.
func (suite *SellRewardsTestSuite) TestAuthzClaimRewards() {
	ctx := context.Background()

	// Get Channel ID
	gaiaChans, err := suite.r.GetChannels(ctx, suite.eRep, suite.gaia.Config().ChainID)
	suite.Require().NoError(err)
	gaiaChannel := gaiaChans[0]
	osmosisChannel := gaiaChans[0].Counterparty

	// IBC Denom for atom on osmosis
	atomOnOsmo := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom(osmosisChannel.PortID, osmosisChannel.ChannelID, suite.gaia.Config().Denom))
	atomOnOsmoDenom := atomOnOsmo.IBCDenom()
	fmt.Printf("Gaia (ATOM) token denom on Osmosis: %s\n", atomOnOsmoDenom)

	// Create and Fund User Wallets
	fundAmount := sdkmath.NewInt(10000000000)

	valAddrB, err := suite.gaia.GetAddress(ctx, "validator")
	suite.Require().NoError(err)
	valoperBech32, err := bech32.ConvertAndEncode("cosmosvaloper", valAddrB)
	suite.Require().NoError(err)
	valBech32, err := bech32.ConvertAndEncode("cosmos", valAddrB)
	suite.Require().NoError(err)
	//valAddr, err := sdk.ValAddressFromBech32(valoperBech32)
	valAddr, err := sdk.AccAddressFromBech32(valBech32)
	suite.Require().NoError(err)

	// The validator address exists from node initialization. Do not recreate, just send funds.
	err = suite.gaia.SendFunds(ctx, interchaintest.FaucetAccountKeyName, ibc.WalletAmount{
		Address: valBech32,
		Amount:  fundAmount,
		Denom:   suite.gaia.Config().Denom,
	})
	suite.Require().NoError(err)

	gaiaUserKeyName := "executor"
	gaiaUserKeyNameOsmosis := "gaiaexecosmo"

	// Create a new mnemonic that we will use for submitting TXs on both gaia and osmosis
	mnemonicAny := genMnemonic(suite.T())
	gaiaUser, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, gaiaUserKeyName, mnemonicAny, fundAmount.Int64(), suite.gaia)
	suite.Require().NoError(err)

	// Import the same user into an osmosis keychain as well
	osmoGaiaUser, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, gaiaUserKeyNameOsmosis, mnemonicAny, fundAmount.Int64(), suite.osmosis)
	suite.Require().NoError(err)
	gaiaUserAddressOsmosis := sdk.MustBech32ifyAddressBytes(suite.osmosis.Config().Bech32Prefix, osmoGaiaUser.Address())

	mnemonicAny = genMnemonic(suite.T())
	gaiaXcsUser, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "xcs", mnemonicAny, fundAmount.Int64(), suite.gaia)
	suite.Require().NoError(err)

	mnemonicAny = genMnemonic(suite.T())
	osmoPoolCreator, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "osmoexec", mnemonicAny, fundAmount.Int64(), suite.osmosis)
	suite.Require().NoError(err)

	valSigner := &helpers.CosmosUser{Address: valAddrB, FromName: "validator"}

	// Submit the TXs to grant another user ability to withdraw rewards
	suite.submitRewardsGrants(valAddrB, gaiaUser.Address(), valSigner)

	// Check how many rewards are currently available to be claimed
	broadcaster := cosmos.NewBroadcaster(suite.T(), suite.gaia.(*cosmos.CosmosChain))
	queryCtx, err := broadcaster.GetClientContext(ctx, gaiaUser)
	suite.Require().NoError(err)
	distClient := disttypes.NewQueryClient(queryCtx)
	bankClient := banktypes.NewQueryClient(queryCtx)

	// delegatorRewardsResp, err := distClient.DelegationTotalRewards(ctx, &disttypes.QueryDelegationTotalRewardsRequest{
	// 	DelegatorAddress: gaiaUser.FormattedAddress(),
	// })
	//suite.Require().NoError(err)

	balancesBeforeClaim, expectedCommisionMap, err := helpers.UnclaimedValidatorCommissions(distClient, bankClient, ctx, valoperBech32, valAddr)
	suite.Require().NoError(err)

	// Withdraw rewards
	txResp := suite.claimAllRewards(gaiaUser.FormattedAddress(), valoperBech32, gaiaUser)

	// Get fee info. TODO: do this more efficiently.
	req := &txTypes.GetTxRequest{Hash: txResp.TxHash}
	txQueryClient := txTypes.NewServiceClient(queryCtx)
	txRes, err := txQueryClient.GetTx(ctx, req)
	suite.Require().NoError(err)
	claimTx := txRes.Tx

	// Validate the balances are higher for the claimed commissions
	postClaimBalanceCheck, err := helpers.ValidatePostClaimBalances(queryCtx.Codec, bankClient, valAddr, valBech32, claimTx, balancesBeforeClaim, expectedCommisionMap)
	suite.Require().NoError(err)
	suite.Require().True(postClaimBalanceCheck)

	// mnemonicAny = genMnemonic(suite.T())
	// osmosisUser, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "recipient", mnemonicAny, fundAmount.Int64(), suite.osmosis)
	// suite.Require().NoError(err)

	// Set SDK context to the right bech32 gaiaBech32Prefix
	gaiaBech32Prefix := suite.gaia.Config().Bech32Prefix
	osmoBech32Prefix := suite.osmosis.Config().Bech32Prefix

	done := SetSDKConfigContext(gaiaBech32Prefix)
	done()

	err = suite.r.StartRelayer(ctx, suite.eRep, ibcPath)
	suite.Require().NoError(err)

	// Prepare to send funds from gaia to osmosis for the gaia validator address
	amountToSend := sdkmath.NewInt(1_000)
	amountToSendXcs := sdkmath.NewInt(1_000)
	osmoPoolCreatorFund := sdkmath.NewInt(1100000)

	gaiaUserAddressCosmos := sdk.MustBech32ifyAddressBytes(gaiaBech32Prefix, gaiaUser.Address())
	gaiaXcsDstAddressOsmosis := sdk.MustBech32ifyAddressBytes(suite.osmosis.Config().Bech32Prefix, gaiaXcsUser.Address())
	gaiaXcsOriginAddress := sdk.MustBech32ifyAddressBytes(gaiaBech32Prefix, gaiaXcsUser.Address())

	gaiaHeight, err := suite.gaia.Height(ctx)
	suite.Require().NoError(err)

	// Send funds from gaia to osmosis for the osmosis pool creator (to have uatom)
	osmoDstAddress := osmoPoolCreator.FormattedAddress()

	var eg, eg2 errgroup.Group
	var gaiaTx, gaiaXcsTx, osmoTx ibc.Tx

	// Confirm the Osmosis user has no atom (prior to IBC transfer)
	gaiaUserAddressOsmosisAtomBalance, err := suite.osmosis.GetBalance(ctx, gaiaUserAddressOsmosis, atomOnOsmoDenom)
	suite.Require().NoError(err)
	suite.Require().True(gaiaUserAddressOsmosisAtomBalance.Equal(sdkmath.ZeroInt()))

	eg.Go(func() error {
		gaiaTx, err = suite.gaia.SendIBCTransfer(ctx, gaiaChannel.ChannelID, gaiaUser.KeyName(), ibc.WalletAmount{
			Address: gaiaUserAddressOsmosis,
			Denom:   suite.gaia.Config().Denom,
			Amount:  amountToSend,
		},
			ibc.TransferOptions{},
		)
		if err != nil {
			return err
		}
		if err := gaiaTx.Validate(); err != nil {
			return err
		}

		_, err = testutil.PollForAck(ctx, suite.gaia, gaiaHeight, gaiaHeight+20, gaiaTx.Packet)
		return err
	})

	eg.Go(func() error {
		osmoTx, err = suite.gaia.SendIBCTransfer(ctx, gaiaChannel.ChannelID, gaiaUser.KeyName(), ibc.WalletAmount{
			Address: osmoDstAddress,
			Denom:   suite.gaia.Config().Denom,
			Amount:  osmoPoolCreatorFund,
		},
			ibc.TransferOptions{},
		)
		if err != nil {
			return err
		}
		if err := osmoTx.Validate(); err != nil {
			return err
		}

		_, err = testutil.PollForAck(ctx, suite.gaia, gaiaHeight, gaiaHeight+20, osmoTx.Packet)
		return err
	})

	suite.Require().NoError(eg.Wait())

	crosschainSwapContractAddr := suite.configureOsmosis(osmoPoolCreator, gaiaChannel, osmosisChannel)
	xcsWasmMemo, err := wasm.CrosschainSwapMemo(gaiaXcsDstAddressOsmosis, gaiaXcsOriginAddress, "uosmo", crosschainSwapContractAddr)
	suite.Require().NoError(err)

	// IBC Denom for uosmo on gaia
	osmoOnGaia := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom(osmosisChannel.PortID, osmosisChannel.ChannelID, suite.osmosis.Config().Denom))
	osmoOnGaiaDenom := osmoOnGaia.IBCDenom()

	// Confirm that the gaia XCS recipient currently has no OSMO tokens (we haven't performed the crosschain swap yet)
	gaiaOsmoBalance, err := suite.gaia.GetBalance(ctx, gaiaXcsOriginAddress, osmoOnGaiaDenom)
	suite.Require().NoError(err)
	suite.Require().True(gaiaOsmoBalance.Equal(sdkmath.ZeroInt()))

	// Confirm that the direct XCS recipient (no IBC) currently has the expected amount of uosmo
	gaiaUserAddressOsmosisBalance, err := suite.osmosis.GetBalance(ctx, gaiaUserAddressOsmosis, "uosmo")
	suite.Require().NoError(err)
	suite.Require().True(gaiaUserAddressOsmosisBalance.Equal(fundAmount))

	// And has 'amountToSend' ATOM tokens from the prior IBC transfer
	gaiaUserAddressOsmosisAtomBalance, err = suite.osmosis.GetBalance(ctx, gaiaUserAddressOsmosis, atomOnOsmoDenom)
	suite.Require().NoError(err)
	suite.Require().True(gaiaUserAddressOsmosisAtomBalance.Equal(amountToSend))

	// Perform a swap by submitting a TX on Osmosis (not via IBC)
	msg := wasm.ExecuteMsg{
		Xcsv2Swap: &wasm.CrosschainSwap{
			OutputDenom: "uosmo",
			Receiver:    gaiaUserAddressCosmos,
			Slippage: wasm.Slippage{
				Twap: wasm.Twap{
					Percentage: "5",
					Window:     10,
				},
			},
			OnFailedDelivery: wasm.OnFailedDelivery{
				RecoveryAddress: gaiaUserAddressOsmosis,
			},
		},
	}

	b, _ := json.Marshal(msg)
	fmt.Println(string(b))

	xcsDirectSwap := &types.MsgExecuteContract{
		Sender:   gaiaUserAddressOsmosis,
		Contract: crosschainSwapContractAddr,
		Msg:      b,
		Funds: []sdk.Coin{
			sdk.NewCoin(atomOnOsmoDenom, amountToSend),
		},
	}

	done = SetSDKConfigContext(osmoBech32Prefix)
	osmosisBroadcaster := cosmos.NewBroadcaster(suite.T(), suite.osmosis.(*cosmos.CosmosChain))

	// Ensure gas is sufficient
	osmosisBroadcaster.ConfigureFactoryOptions(func(factory tx.Factory) tx.Factory {
		factory = factory.WithGas(2000000)
		return factory
	})

	// Confirm that the gaia direct XCS recipient has NO osmo prior to the swap
	gaiaOsmoBalance, err = suite.gaia.GetBalance(ctx, gaiaUserAddressCosmos, osmoOnGaiaDenom)
	suite.Require().NoError(err)
	suite.Require().True(gaiaOsmoBalance.Equal(sdkmath.ZeroInt()))

	osmoXcsDirectResp, err := cosmos.BroadcastTx(ctx, osmosisBroadcaster, osmoGaiaUser, xcsDirectSwap)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), osmoXcsDirectResp)
	osmoXcsDirectReq := &txTypes.GetTxRequest{Hash: osmoXcsDirectResp.TxHash}
	osmoQueryCtx, err := osmosisBroadcaster.GetClientContext(ctx, osmoGaiaUser)

	osmoTxQueryClient := txTypes.NewServiceClient(osmoQueryCtx)
	osmoTxRes, err := osmoTxQueryClient.GetTx(ctx, osmoXcsDirectReq)
	suite.Require().NoError(err)
	osmoXcsTx := osmoTxRes.Tx
	fmt.Printf("%+v\n", osmoXcsTx)
	done()
	done = SetSDKConfigContext(gaiaBech32Prefix)
	defer done()

	// Wait a couple blocks to allow the crosschain swap IBC transfer to propagate
	suite.Require().NoError(testutil.WaitForBlocks(ctx, 2, suite.gaia, suite.osmosis))

	// Confirm that the gaia direct XCS recipient now has some OSMO tokens (we performed the crosschain swap)
	gaiaOsmoBalance, err = suite.gaia.GetBalance(ctx, gaiaUserAddressCosmos, osmoOnGaiaDenom)
	suite.Require().NoError(err)
	suite.Require().True(gaiaOsmoBalance.GT(sdkmath.ZeroInt()))

	gaiaHeight, err = suite.gaia.Height(ctx)
	suite.Require().NoError(err)

	// Get the balance before the 2nd XCSv2 swap
	gaiaOsmoBalanceXcsInitial, err := suite.gaia.GetBalance(ctx, gaiaXcsOriginAddress, osmoOnGaiaDenom)
	suite.Require().NoError(err)

	eg2.Go(func() error {
		gaiaXcsTx, err = suite.gaia.SendIBCTransfer(ctx, gaiaChannel.ChannelID, gaiaXcsUser.KeyName(), ibc.WalletAmount{
			Address: crosschainSwapContractAddr,
			Denom:   suite.gaia.Config().Denom,
			Amount:  amountToSendXcs,
		},
			ibc.TransferOptions{
				Memo: xcsWasmMemo,
			},
		)
		if err != nil {
			return err
		}
		if err := gaiaXcsTx.Validate(); err != nil {
			return err
		}

		fmt.Println(gaiaXcsTx.TxHash)
		_, err = testutil.PollForAck(ctx, suite.gaia, gaiaHeight, gaiaHeight+20, gaiaXcsTx.Packet)
		return err
	})

	suite.Require().NoError(eg2.Wait())

	// Wait a couple blocks to allow the crosschain swap IBC transfer to propagate
	suite.Require().NoError(testutil.WaitForBlocks(ctx, 2, suite.gaia, suite.osmosis))

	// Get the balance after the 2nd XCSv2 swap and confirm it has now increased due to the swap
	gaiaOsmoBalanceXcsFinal, err := suite.gaia.GetBalance(ctx, gaiaXcsOriginAddress, osmoOnGaiaDenom)
	suite.Require().NoError(err)
	suite.Require().True(gaiaOsmoBalanceXcsFinal.GT(gaiaOsmoBalanceXcsInitial))

	err = suite.r.StopRelayer(ctx, suite.eRep)
	suite.Require().NoError(err)
}

func (suite *SellRewardsTestSuite) SetupTest() {
	sdk.SetAddrCacheEnabled(false)
	os.Setenv("IBCTEST_SKIP_FAILURE_CLEANUP", "true")
	suite.logger = logging.DoConfigureLogger("DEBUG")
	var err error

	nv := 1
	nf := 0

	// Start the network
	spec := []*interchaintest.ChainSpec{
		{Name: "gaia", ChainName: "gaia", Version: "v14.1.0", NumValidators: &nv, NumFullNodes: &nf},
		{Name: "osmosis", ChainName: "osmosis", Version: "v20.5.0", NumValidators: &nv, NumFullNodes: &nf},
	}

	spec[0].ChainConfig.GasPrices = "1uatom"
	spec[1].ChainConfig.GasPrices = "1uosmo"

	configTomlOverridesGaia := make(testutil.Toml)
	configTomlOverridesOsmo := make(testutil.Toml)
	configTomlOverridesGaia["log_level"] = "debug"
	configTomlOverridesOsmo["log_level"] = "debug"
	configTomlOverridesOsmo["gas_prices"] = "0uosmo"
	configTomlOverridesGaia["gas_prices"] = "0uatom"

	configFileOverridesGaia := make(map[string]any)
	configFileOverridesGaia["config/config.toml"] = configTomlOverridesGaia
	configFileOverridesOsmo := make(map[string]any)
	configFileOverridesOsmo["config/config.toml"] = configTomlOverridesOsmo
	spec[0].ConfigFileOverrides = configFileOverridesGaia
	spec[1].ConfigFileOverrides = configFileOverridesOsmo

	// Chain Factory
	cf := interchaintest.NewBuiltinChainFactory(suite.logger, spec)

	chains, err := cf.Chains(suite.T().Name())
	require.NoError(suite.T(), err)
	suite.gaia, suite.osmosis = chains[0], chains[1]

	// Relayer Factory
	suite.client, suite.network = interchaintest.DockerSetup(suite.T())
	suite.r = interchaintest.NewBuiltinRelayerFactory(ibc.CosmosRly, suite.logger).Build(suite.T(), suite.client, suite.network)

	// Prep Interchain
	ic := interchaintest.NewInterchain().
		AddChain(suite.gaia).
		AddChain(suite.osmosis).
		AddRelayer(suite.r, "relayer").
		AddLink(interchaintest.InterchainLink{
			Chain1:  suite.gaia,
			Chain2:  suite.osmosis,
			Relayer: suite.r,
			Path:    ibcPath,
		})

	// Reporter/logs
	suite.rep = testreporter.NewNopReporter()
	suite.eRep = suite.rep.RelayerExecReporter(suite.T())
	suite.ic = ic

	// In order to authorize the Claim grant we need to know the validator key
	suite.gaiaValidatorMnemonic = genMnemonic(suite.T())
	cosmosGaia := suite.gaia.(*cosmos.CosmosChain)
	cosmosGaia.SetValidatorMnemonic(suite.gaiaValidatorMnemonic)

	dbDir := interchaintest.TempDir(suite.T())
	dbPath := filepath.Join(dbDir, "blocks.db")
	fmt.Printf("SQLite DB path: %s\n", dbPath)

	// Build interchain
	suite.Require().NoError(suite.ic.Build(context.Background(), suite.eRep, interchaintest.InterchainBuildOptions{
		TestName:          suite.T().Name(),
		Client:            suite.client,
		NetworkID:         suite.network,
		SkipPathCreation:  false,
		BlockDatabaseFile: dbPath,
	}))

	// Make sure codec is registered, since it is not by default
	authz.RegisterInterfaces(suite.gaia.Config().EncodingConfig.InterfaceRegistry)
	disttypes.RegisterInterfaces(suite.gaia.Config().EncodingConfig.InterfaceRegistry)
	// Without this, we will get "unable to get type url /cosmwasm.wasm.v1.MsgInstantiateContract"
	types.RegisterInterfaces(suite.osmosis.Config().EncodingConfig.InterfaceRegistry)
	clienttypes.RegisterInterfaces(suite.gaia.Config().EncodingConfig.InterfaceRegistry)
	clienttypes.RegisterInterfaces(suite.osmosis.Config().EncodingConfig.InterfaceRegistry)

	//suite.gaia.Config().EncodingConfig.InterfaceRegistry.RegisterImplementations((*ibcexported.ClientState)(nil), &clienttypes.ClientS)
}

var sdkConfigMutex sync.Mutex

// SetSDKContext sets the SDK config to the given bech32 prefixes
func SetSDKConfigContext(prefix string) func() {
	sdkConfigMutex.Lock()
	sdkConf := sdk.GetConfig()
	sdkConf.SetBech32PrefixForAccount(prefix, prefix+"pub")
	sdkConf.SetBech32PrefixForValidator(prefix+"valoper", prefix+"valoperpub")
	sdkConf.SetBech32PrefixForConsensusNode(prefix+"valcons", prefix+"valconspub")
	return sdkConfigMutex.Unlock
}

func genMnemonic(t *testing.T) string {
	// read entropy seed straight from tmcrypto.Rand and convert to mnemonic
	entropySeed, err := bip39.NewEntropy(256)
	if err != nil {
		t.Fail()
	}

	mn, err := bip39.NewMnemonic(entropySeed)
	if err != nil {
		t.Fail()
	}

	return mn
}

// Required for go test to run this suite
func TestSellRewardsTestSuite(t *testing.T) {
	suite.Run(t, new(SellRewardsTestSuite))
}

func (suite *SellRewardsTestSuite) submitRewardsGrants(validator sdk.AccAddress, grantee sdk.AccAddress, signer cosmos.User) {
	oneDay := time.Now().Add(24 * time.Hour)
	msgGrantCommission, err := authz.NewMsgGrant(validator, grantee, authz.NewGenericAuthorization(withdrawCommission), &oneDay)
	suite.Require().NoError(err)
	msgGrantReward, err := authz.NewMsgGrant(validator, grantee, authz.NewGenericAuthorization(withdrawReward), &oneDay)
	suite.Require().NoError(err)
	broadcaster := cosmos.NewBroadcaster(suite.T(), suite.gaia.(*cosmos.CosmosChain))
	// Ensure gas is sufficient
	broadcaster.ConfigureFactoryOptions(func(factory tx.Factory) tx.Factory {
		factory = factory.WithGas(700000)
		return factory
	})

	resp, err := cosmos.BroadcastTx(context.Background(), broadcaster, signer, msgGrantCommission, msgGrantReward)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), resp)
}

func (suite *SellRewardsTestSuite) claimAllRewards(grantee, valAddr string, signer ibc.Wallet) *sdk.TxResponse {
	claimMsg := disttypes.NewMsgWithdrawValidatorCommission(valAddr)
	claimMsgBytes, err := claimMsg.Marshal()
	if err != nil {
		return nil
	}

	authzMsgClaim := &authz.MsgExec{
		Grantee: grantee,
		Msgs:    []*ctypes.Any{{TypeUrl: withdrawCommission, Value: claimMsgBytes}},
	}

	ctx := context.Background()
	broadcaster := cosmos.NewBroadcaster(suite.T(), suite.gaia.(*cosmos.CosmosChain))

	// Ensure gas is sufficient
	broadcaster.ConfigureFactoryOptions(func(factory tx.Factory) tx.Factory {
		factory = factory.WithGas(700000)
		return factory
	})

	resp, err := cosmos.BroadcastTx(ctx, broadcaster, signer, authzMsgClaim)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), resp)
	return &resp
}

func (suite *SellRewardsTestSuite) TestClaimRewards() {
	addr := "cosmos..."
	claimMsg := disttypes.NewMsgWithdrawValidatorCommission(addr)
	claimMsgBytes, err := claimMsg.Marshal()
	if err != nil {
		return
	}

	// Grantee is the validator that allows us to claim on their behalf (funds go to the validator address)
	authzMsgClaim := &authz.MsgExec{
		Grantee: suite.osmosisTxSigner.Address.String(),
		Msgs:    []*ctypes.Any{{TypeUrl: withdrawCommission, Value: claimMsgBytes}},
	}

	ctx := context.Background()
	broadcaster := cosmosclient.NewBroadcaster(suite.osmosisChainClient)

	// Crosschain swaps use about this much gas
	broadcaster.ConfigureFactoryOptions(func(factory tx.Factory) tx.Factory {
		factory = factory.WithGas(700000)
		return factory
	})

	resp, err := cosmosclient.BroadcastTx(ctx, broadcaster, &suite.osmosisTxSigner, authzMsgClaim)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), resp)
}

// Prerequisite: poolCreator must have 1000000000uosmo plus the pool's initial deposit in their account
func (suite *SellRewardsTestSuite) configureOsmosis(poolCreator ibc.Wallet, gaiaChannel ibc.ChannelOutput, osmoChannel ibc.ChannelCounterparty) (
	xcsv2ContractAddr string,
) {
	ctx := context.Background()
	osmo := suite.osmosis.(*cosmos.CosmosChain)
	gaia := suite.gaia.(*cosmos.CosmosChain)

	// Set SDK context to the right bech32 prefix
	done := SetSDKConfigContext(osmo.Config().Bech32Prefix)
	defer done()

	// Find all denoms in the osmo user's balance
	broadcaster := cosmos.NewBroadcaster(suite.T(), osmo)
	queryCtx, err := broadcaster.GetClientContext(ctx, poolCreator)
	suite.Require().NoError(err)
	bankClient := banktypes.NewQueryClient(queryCtx)

	suite.Require().NoError(err)
	queryBalReq := banktypes.NewQueryAllBalancesRequest(poolCreator.Address(), &query.PageRequest{}, true)
	balancesResp, err := bankClient.AllBalances(ctx, queryBalReq)
	suite.Require().NoError(err)
	weights := ""
	initialDeposit := ""
	var ibcDenom string

	for i, res := range balancesResp.Balances {
		// weights are equal so initial deposit should be equal as well
		depositAmount := balancesResp.Balances[0].Amount.Quo(sdkmath.NewIntFromUint64(2))
		weights = weights + "1" + res.Denom

		if strings.HasPrefix(res.Denom, "ibc") {
			ibcDenom = res.Denom
		}

		initialDeposit = initialDeposit + depositAmount.String() + res.Denom
		if i < len(balancesResp.Balances)-1 {
			weights = weights + ","
			initialDeposit = initialDeposit + ","
		}
	}

	suite.Require().NotEmpty(ibcDenom)

	// Deploy gamm pool
	_, err = cosmos.OsmosisCreatePool(osmo, ctx, poolCreator.KeyName(), cosmos.OsmosisPoolParams{
		Weights:        weights,
		InitialDeposit: initialDeposit,
		SwapFee:        "0.01",
		ExitFee:        "0.00", // In Osmosis v20 this must be 0
		FutureGovernor: "168h",
	})
	suite.Require().NoError(err)

	poolsIds, err := cosmos.OsmosisQueryPoolIds(osmo, ctx, weights, "Balancer")
	suite.Require().NoError(err)
	fmt.Printf("%+v\n", poolsIds)

	newPoolId := poolsIds[0]

	// Deploy swaprouter contract (dependency of the crosschain swap contract)
	swapRouterCodeId, err := osmo.StoreContract(ctx, poolCreator.KeyName(), "./artifacts/swaprouter.wasm")
	suite.Require().NoError(err)
	poolCreatorAddr := poolCreator.FormattedAddress()
	initSwapRouterMsg := fmt.Sprintf(`{"owner": "%s"}`, poolCreatorAddr)
	swapRouterContractAddr, err := osmo.InstantiateContract(ctx, poolCreator.KeyName(), swapRouterCodeId, initSwapRouterMsg, false, "--admin", poolCreatorAddr)
	suite.Require().NoError(err)

	// Add our test pool to the swaprouter contract
	msgSetRoute := wasm.ExecuteMsg{
		SetRoute: &wasm.SwapRouterSetRoute{
			InputDenom:  ibcDenom,
			OutputDenom: "uosmo",
			PoolRoutes: []wasm.PoolRoute{{
				PoolID:        strconv.FormatInt(int64(newPoolId), 10),
				TokenOutDenom: "uosmo",
			}},
		},
	}

	b, _ := json.Marshal(msgSetRoute)
	fmt.Println(string(b))

	reqSetRoute := &types.MsgExecuteContract{
		Sender:   poolCreatorAddr,
		Contract: swapRouterContractAddr,
		Msg:      b,
	}

	// Ensure gas is sufficient
	broadcaster.ConfigureFactoryOptions(func(factory tx.Factory) tx.Factory {
		factory = factory.WithGas(700000)
		return factory
	})

	resp, err := cosmos.BroadcastTx(ctx, broadcaster, poolCreator, reqSetRoute)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), resp)

	// Deploy registry contract (dependency of the crosschain swap contract)
	registryCodeId, err := osmo.StoreContract(ctx, poolCreator.KeyName(), "./artifacts/crosschain_registry.wasm")
	suite.Require().NoError(err)
	initRegistryMsg := fmt.Sprintf(`{"owner": "%s"}`, poolCreatorAddr)
	registryContractAddr, err := osmo.InstantiateContract(ctx, poolCreator.KeyName(), registryCodeId, initRegistryMsg, false, "--admin", poolCreatorAddr)
	suite.Require().NoError(err)

	// Deploy crosschain swap v2 contract
	xcsv2CodeId, err := osmo.StoreContract(ctx, poolCreator.KeyName(), "./artifacts/crosschain_swaps.wasm")
	suite.Require().NoError(err)
	initXcsv2Msg := fmt.Sprintf(`{"swap_contract": "%s", "registry_contract": "%s", "governor": "%s"}`, swapRouterContractAddr, registryContractAddr, poolCreatorAddr)
	xcsv2ContractAddr, err = osmo.InstantiateContract(ctx, poolCreator.KeyName(), xcsv2CodeId, initXcsv2Msg, false, "--admin", poolCreatorAddr)
	suite.Require().NoError(err)
	fmt.Printf("XCSv2 contract address: %s\n", xcsv2ContractAddr)

	// Configure registry contract -- set the chain name to bech32 prefix mappings
	msgModifyBech32Prefixes := wasm.ExecuteMsg{
		ModifyBech32Prefixes: &wasm.ModifyBech32Prefixes{
			Operations: []wasm.ChainToBech32PrefixInput{
				{
					Operation: "set",
					ChainName: osmo.Config().Name,
					Prefix:    osmo.Config().Bech32Prefix,
				},
				{
					Operation: "set",
					ChainName: gaia.Config().Name,
					Prefix:    gaia.Config().Bech32Prefix,
				},
			},
		},
	}

	b, _ = json.Marshal(msgModifyBech32Prefixes)
	fmt.Println(string(b))

	reqModifyBech32Prefix := &types.MsgExecuteContract{
		Sender:   poolCreatorAddr,
		Contract: registryContractAddr,
		Msg:      b,
	}

	resp, err = cosmos.BroadcastTx(ctx, broadcaster, poolCreator, reqModifyBech32Prefix)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), resp)

	// Configure registry contract -- set the channel mappings
	msgChannelMappings := wasm.ExecuteMsg{
		ModifyChainChannelLinks: &wasm.ModifyChainChannelLinks{
			Operations: []wasm.ConnectionInput{
				{
					Operation:   "set",
					SourceChain: gaia.Config().Name,
					DestChain:   osmo.Config().Name,
					ChannelID:   gaiaChannel.ChannelID,
				},
				{
					Operation:   "set",
					SourceChain: osmo.Config().Name,
					DestChain:   gaia.Config().Name,
					ChannelID:   osmoChannel.ChannelID,
				},
			},
		},
	}

	b, _ = json.Marshal(msgChannelMappings)
	fmt.Println(string(b))

	reqSetChannelMappings := &types.MsgExecuteContract{
		Sender:   poolCreatorAddr,
		Contract: registryContractAddr,
		Msg:      b,
	}

	resp, err = cosmos.BroadcastTx(ctx, broadcaster, poolCreator, reqSetChannelMappings)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), resp)
	return
}

func assertTransactionIsValid(t *testing.T, resp sdk.TxResponse) {
	t.Helper()
	require.NotNil(t, resp)
	require.NotEqual(t, 0, resp.GasUsed)
	require.NotEqual(t, 0, resp.GasWanted)
	require.Equal(t, uint32(0), resp.Code)
	require.NotEmpty(t, resp.Data)
	require.NotEmpty(t, resp.TxHash)
	require.NotEmpty(t, resp.Events)
}
