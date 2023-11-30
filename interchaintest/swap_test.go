package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/math"
	sdkmath "cosmossdk.io/math"
	"github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/KyleMoser/Cronmos/helpers"
	"github.com/KyleMoser/Cronmos/logging"
	"github.com/KyleMoser/Cronmos/wasm"
	registry "github.com/KyleMoser/cosmos-client/client/chain_registry"

	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	"github.com/KyleMoser/cosmos-client/cmd"
	"github.com/cosmos/cosmos-sdk/client/tx"
	ctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"

	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	disttypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/cosmos/go-bip39"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
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
	osmosisHome        = "HOME GOES HERE. TODO: LOOKUP BASED ON PROFILE"
	withdrawCommission = "/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission"
	withdrawReward     = "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward"
)

type SellRewardsTestSuite struct {
	suite.Suite
	logger *zap.Logger
	//gaiaChainClient          *cosmosclient.ChainClient
	osmosisChainClient       *cosmosclient.ChainClient
	chainClientConfigOsmosis *cosmosclient.ChainClientConfig
	osmosisTxSigner          helpers.CosmosUser
	osmosis                  ibc.Chain
	gaia                     ibc.Chain
	r                        ibc.Relayer
	rep                      *testreporter.Reporter
	eRep                     *testreporter.RelayerExecReporter
	ic                       *interchaintest.Interchain
	client                   *client.Client
	network                  string
	gaiaValidatorMnemonic    string
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

	// Create and Fund User Wallets
	fundAmount := sdkmath.NewInt(10_000_000)
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

	mnemonicAny := genMnemonic(suite.T())
	gaiaUser, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "executor", mnemonicAny, fundAmount.Int64(), suite.gaia)
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

	valCommissionResp, err := distClient.ValidatorCommission(
		ctx,
		&disttypes.QueryValidatorCommissionRequest{ValidatorAddress: valoperBech32},
	)
	suite.Require().NoError(err)
	unclaimedCommission := valCommissionResp.GetCommission()
	unclaimedCommissionCoins := unclaimedCommission.GetCommission()

	// Check the current bank balance for the validator, for the denoms we can claim as commission
	balancesBeforeClaim := map[string]sdk.Coin{}
	expectedCommisionMap := map[string]sdkmath.Int{}

	for _, unclaimedCommissionCoin := range unclaimedCommissionCoins {
		suite.Require().NoError(err)
		queryBalReq := banktypes.NewQueryBalanceRequest(valAddr, unclaimedCommissionCoin.Denom)
		res, err := bankClient.Balance(ctx, queryBalReq)
		suite.Require().NoError(err)
		balancesBeforeClaim[unclaimedCommissionCoin.Denom] = *res.Balance
		expectedCommisionMap[unclaimedCommissionCoin.Denom] = unclaimedCommissionCoin.Amount.TruncateInt()
	}

	// Withdraw rewards
	txResp := suite.claimAllRewards(gaiaUser.FormattedAddress(), valoperBech32, gaiaUser)

	// Get fee info. TODO: do this more efficiently.
	req := &txTypes.GetTxRequest{Hash: txResp.TxHash}
	txQueryClient := txTypes.NewServiceClient(queryCtx)
	txRes, err := txQueryClient.GetTx(ctx, req)
	suite.Require().NoError(err)
	claimTx := txRes.Tx

	// Check that the new balance is greater than the previous balance
	for denom, balanceBefore := range balancesBeforeClaim {
		suite.Require().NoError(err)
		queryBalReq := banktypes.NewQueryBalanceRequest(valAddr, denom)
		res, err := bankClient.Balance(ctx, queryBalReq)
		suite.Require().NoError(err)
		balanceAfter := res.Balance.Amount
		suite.Require().True(balanceAfter.GT(balanceBefore.Amount))

		feePaid := claimTx.GetFee()
		isFeeDenom, feeCoin := feePaid.Find(denom)
		expectedCommission := expectedCommisionMap[denom]

		changeInBalance := balanceAfter.Sub(balanceBefore.Amount)
		if isFeeDenom {
			expectedCommission = expectedCommission.Sub(feeCoin.Amount)
		}

		// Assert that the balance increased by at least the validator commission we projected (minus TX fees)
		suite.Require().True(changeInBalance.GT(expectedCommission))

		// No claim, and balance doesn't change
		res, err = bankClient.Balance(ctx, queryBalReq)
		suite.Require().NoError(err)
		suite.Require().True(balanceAfter.Equal(res.Balance.Amount))
	}

	// mnemonicAny = genMnemonic(suite.T())
	// osmosisUser, err := interchaintest.GetAndFundTestUserWithMnemonic(ctx, "recipient", mnemonicAny, fundAmount.Int64(), suite.osmosis)
	// suite.Require().NoError(err)

	// Set SDK context to the right bech32 prefix
	prefix := suite.gaia.Config().Bech32Prefix
	done := SetSDKConfigContext(prefix)
	done()

	err = suite.r.StartRelayer(ctx, suite.eRep, ibcPath)
	suite.Require().NoError(err)

	// Prepare to send funds from gaia to osmosis for the gaia validator address
	amountToSend := sdkmath.NewInt(1_000)
	gaiaDstAddress := sdk.MustBech32ifyAddressBytes(suite.osmosis.Config().Bech32Prefix, gaiaUser.Address())
	gaiaHeight, err := suite.gaia.Height(ctx)
	suite.Require().NoError(err)

	var eg errgroup.Group
	var gaiaTx ibc.Tx

	// TODO: this needs to be modified for an authz ibc transfer
	eg.Go(func() error {
		gaiaTx, err = suite.gaia.SendIBCTransfer(ctx, gaiaChannel.ChannelID, gaiaUser.KeyName(), ibc.WalletAmount{
			Address: gaiaDstAddress,
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

	suite.Require().NoError(err)
	suite.Require().NoError(eg.Wait())

	// Trace IBC Denom
	gaiaDenomTrace := transfertypes.ParseDenomTrace(transfertypes.GetPrefixedDenom(osmosisChannel.PortID, osmosisChannel.ChannelID, suite.gaia.Config().Denom))
	gaiaIbcDenom := gaiaDenomTrace.IBCDenom()

	// Test destination wallets have increased funds.
	// TODO: actually we need to check the USDC balance for this address.
	gaiaIBCBalance, err := suite.osmosis.GetBalance(ctx, gaiaDstAddress, gaiaIbcDenom)
	suite.Require().NoError(err)
	suite.Require().True(gaiaIBCBalance.GT(sdkmath.ZeroInt()))
	err = suite.r.StopRelayer(ctx, suite.eRep)
	suite.Require().NoError(err)
}

func (suite *SellRewardsTestSuite) SetupTest() {
	suite.logger = logging.DoConfigureLogger("DEBUG")
	var err error

	nv := 1
	nf := 0

	// Start the network
	spec := []*interchaintest.ChainSpec{
		{Name: "gaia", ChainName: "gaia", Version: "v7.0.3", NumValidators: &nv, NumFullNodes: &nf},
		{Name: "osmosis", ChainName: "osmosis", Version: "v14.0.1", NumValidators: &nv, NumFullNodes: &nf},
	}

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

	// Build interchain
	suite.Require().NoError(suite.ic.Build(context.Background(), suite.eRep, interchaintest.InterchainBuildOptions{
		TestName:         suite.T().Name(),
		Client:           suite.client,
		NetworkID:        suite.network,
		SkipPathCreation: false,
	}))

	// Make sure codec is registered, since it is not by default
	authz.RegisterInterfaces(suite.gaia.Config().EncodingConfig.InterfaceRegistry)
	disttypes.RegisterInterfaces(suite.gaia.Config().EncodingConfig.InterfaceRegistry)
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

func (suite *SellRewardsTestSuite) TestRegisterICA() {
	//logger := logging.DoConfigureLogger("DEBUG")
	// chainClientConfigOsmosis, err := registry.GetChain(context.Background(), chainRegOsmosis, logger)
	// require.NoError(t, err)
	// chainClientConfigOsmosis.Modules = cmd.ModuleBasics
	// chainClientConfigCosmosHub, err := registry.GetChain(context.Background(), chainRegCosmosHub, logger)
	// require.NoError(t, err)
	// chainClientConfigCosmosHub.Modules = cmd.ModuleBasics
	// chainClientConfigCosmosHub.Key = "arb"
	//hubHome := "/home/kyle/.gaia"
	//chainClient, err := cosmosclient.NewChainClient(logger, chainClientConfigCosmosHub, hubHome, nil, nil)
	//require.NoError(t, err)
	//broadcaster := cosmosclient.NewBroadcaster(chainClient)
	//ctx := context.Background()
	//defaultAcc, err := chainClient.GetKeyAddress()
	//require.NoError(t, err)
	//user := helpers.CosmosUser{Address: defaultAcc, FromName: chainClientConfigCosmosHub.Key}
	//clientCtx, err := broadcaster.GetClientContext(ctx, &user)
	//require.NoError(t, err)
	//cosmosHubOsmosisConnectionID := "connection-257"
	// queryClient := conntypes.NewQueryClient(clientCtx)
	// key := []byte{}
	// for {
	// 	req := &conntypes.QueryConnectionsRequest{
	// 		Pagination: &query.PageRequest{Key: key},
	// 	}
	// 	res, err := queryClient.Connections(ctx, req)
	// 	require.NoError(t, err)
	// 	key = res.Pagination.NextKey
	// 	if len(key) == 0 {
	// 		break
	// 	}
	// 	for _, conn := range res.Connections {
	// 		//fmt.Printf(conn.Id)
	// 		if conn.Id == "connection-257" {
	// 			fmt.Printf("Found CosmosHub-Osmosis connection!")
	// 			fmt.Printf("%+v\n", conn)
	// 		}
	// 	}
	// }
	//msgRegisterICA := controllertypes.NewMsgRegisterInterchainAccount(cosmosHubOsmosisConnectionID, user.FormattedAddress(), "")
	//resp, err := cosmosclient.BroadcastTx(ctx, broadcaster, &user, msgRegisterICA)
	//require.NoError(t, err)
	//assertTransactionIsValid(t, resp)
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

func (suite *SellRewardsTestSuite) TestConfigureOsmosis() {
	mnemonicAny := genMnemonic(suite.T())
	osmosisKey := "executor"
	fundAmount := sdkmath.NewInt(10_000_000)
	_, err := interchaintest.GetAndFundTestUserWithMnemonic(context.Background(), osmosisKey, mnemonicAny, fundAmount.Int64(), suite.gaia)
	suite.Require().NoError(err)

	// Deploy gamm pool
	// TODO: does uatom exist in our Osmosis chain image?
	numPools, err := cosmos.OsmosisCreatePool(suite.osmosis.(*cosmos.CosmosChain), context.Background(), osmosisKey, cosmos.OsmosisPoolParams{
		Weights:        "4uatom,4osmo",
		InitialDeposit: "1000000000uatom,1000000000osmo",
		SwapFee:        "0.01",
		ExitFee:        "0.01",
		FutureGovernor: "168h",
	})
	suite.Require().NoError(err)
	suite.Require().Equal("1", numPools)

	// Deploy crosschain swap contract

	// Configure crosschain swap contract (to use pool route from above)
}

func (suite *SellRewardsTestSuite) TestSubmitTx() {
	sdkCtxMu := suite.osmosisChainClient.SetSDKContext()
	defer sdkCtxMu()

	var err error
	suite.chainClientConfigOsmosis, err = registry.GetChain(context.Background(), chainRegOsmosis, suite.logger)
	suite.Require().NoError(err)
	suite.chainClientConfigOsmosis.Modules = cmd.ModuleBasics
	suite.chainClientConfigOsmosis.Key = "arb" // This must be set in advance (TODO: remove to make this test easier to run)

	chainClientConfigCosmosHub, err := registry.GetChain(context.Background(), chainRegCosmosHub, suite.logger)
	suite.Require().NoError(err)
	chainClientConfigCosmosHub.Modules = cmd.ModuleBasics

	suite.osmosisChainClient, err = cosmosclient.NewChainClient(suite.logger, suite.chainClientConfigOsmosis, osmosisHome, nil, nil)
	suite.Require().NoError(err)

	defaultAcc, err := suite.osmosisChainClient.GetKeyAddress()
	suite.Require().NoError(err)
	suite.osmosisTxSigner = helpers.CosmosUser{Address: defaultAcc, FromName: suite.chainClientConfigOsmosis.Key}

	inputAmount := "10000"
	amountStr, _ := math.NewIntFromString(inputAmount)
	outputDenom := "ibc/D189335C6E4A68B513C10AB227BF1C1D38C746766278BA3EEB4FB14124F1D858" // AXL USDC

	osmosisSwapForwardContract := "osmo1uwk8xc6q0s6t5qcpr6rht3sczu6du83xq8pwxjua0hfj5hzcnh3sqxwvxs"
	osmosisWalletAddr := "osmo..."
	receiver := "cosmos..." //Who will get the funds on the other chain (should correspond to osmosisWalletAddr above)

	msg := wasm.ExecuteMsg{
		Swap: &wasm.CrosschainSwap{
			OutputDenom: outputDenom,
			Receiver:    receiver,
			Slippage: wasm.Slippage{
				Twap: wasm.Twap{
					Percentage: "5",
					Window:     10,
				},
			},
			OnFailedDelivery: wasm.OnFailedDelivery{
				RecoveryAddress: suite.osmosisTxSigner.Address.String(),
			},
		},
	}

	b, _ := json.Marshal(msg)
	fmt.Println(string(b))

	req := &types.MsgExecuteContract{
		Sender:   osmosisWalletAddr,
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
	resp, err := cosmosclient.BroadcastTx(ctx, broadcaster, &suite.osmosisTxSigner, req)
	suite.Require().NoError(err)
	assertTransactionIsValid(suite.T(), resp)
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
