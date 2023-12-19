package osmosis

import (
	"context"
	"fmt"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/KyleMoser/Cronmos/helpers"
	"github.com/KyleMoser/Cronmos/wasm"
	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	"github.com/KyleMoser/cosmos-client/client/query"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"go.uber.org/zap"
)

// The XCSv2 contract will swap the input token for the output token.
// The output token will be IBC transferred to the origin chain.
func getOutputTokenDestinationChainParams(conf *Xcsv2OriginChainConfig, osmosisConfig *Xcsv2OsmosisConfig) (
	outputDestinationChainClient *cosmosclient.ChainClient,
	outputTokenDestinationAddress string,
	outputTokenDenom string,
) {
	outputDestinationChainClient = conf.OriginChainClient
	outputTokenDestinationAddress = conf.OriginChainTxSignerAddress
	outputTokenDenom = conf.OriginChainSwapOutputDenom

	return
}

func getBalance(destinationCc *cosmosclient.ChainClient, address string, denom string) (sdkmath.Int, error) {
	// Check the current balance of the output token prior to the crosschain swap.
	outputTokenBalanceQuery := query.Query{Client: destinationCc, Options: &query.QueryOptions{}}
	outputTokenBalance, err := outputTokenBalanceQuery.Bank_Balance(address, denom)
	if err != nil {
		return sdkmath.ZeroInt(), err
	}
	return outputTokenBalance.Balance.Amount, nil
}

func crosschainSwap(
	ctx context.Context,
	logger zap.Logger,
	chainClient *cosmosclient.ChainClient,
	crosschainSwapDestAddr,
	destChainInputDenom,
	destChainOutputDenom string) error {

	destChainOutputDenomInitialBalance, err := getBalance(chainClient, crosschainSwapDestAddr, destChainOutputDenom)
	if err != nil {
		return err
	}

	destChainInputDenomInitialBalance, err := getBalance(chainClient, crosschainSwapDestAddr, destChainInputDenom)
	if err != nil {
		return err
	}

	originHeight, err := chainClient.QueryLatestHeight(ctx)
	if err != nil {
		return err
	}

	logger.Info("swap input token initial balance",
		zap.String("chain", chainClient.Config.ChainID),
		zap.String("denom", destChainInputDenom),
		zap.String("amount", destChainInputDenomInitialBalance.String()))

	logger.Info("swap output token initial balance",
		zap.String("chain", chainClient.Config.ChainID),
		zap.String("denom", destChainOutputDenom),
		zap.String("amount", destChainOutputDenomInitialBalance.String()))

	// Create an IBC hooks memo that will invoke the Osmosis crosschain swap (XCVv2) contract
	xcsWasmMemo, err := wasm.CrosschainSwapMemo(conf.OsmosisRecipientAddress, destinationAddress, conf.OsmosisOutputDenom, osmosisSwapForwardContract)
	if err != nil {
		return err
	}

	// Create IBC Transfer message with channel, port, and client ID from IBC chain registry.
	// See https://github.com/cosmos/chain-registry/blob/master/_IBC/cosmoshub-osmosis.json.
	// TODO: move these params out of this configuration and look it up automatically.
	msgTransfer, err := helpers.PrepareIbcTransfer(
		conf.OriginChainClient,
		originHeight,
		conf.OriginToOsmosisSrcChannel,
		conf.OriginToOsmosisSrcPort,
		conf.OriginToOsmosisClientId)
	if err != nil {
		return err
	}

	// Do not allow a trade above the configured maximum
	swapAmount := originInputTokenInitialBalance.Balance.Amount
	if swapAmount.GT(conf.OriginChainTokenInMax) {
		swapAmount = conf.OriginChainTokenInMax
	}

	msgTransfer.Sender = conf.OriginChainTxSignerAddress
	msgTransfer.Receiver = osmosisSwapForwardContract
	msgTransfer.Token = sdk.NewCoin(conf.OriginChainTokenInDenom, swapAmount)
	msgTransfer.Memo = xcsWasmMemo // this will invoke IBC hooks on the recipient chain

	broadcaster := cosmosclient.NewBroadcaster(conf.OriginChainClient)

	originHeightPreXcs, err := conf.OriginChainClient.QueryLatestHeight(conf.ctx)
	if err != nil {
		return err
	}
	desiredHeight := originHeightPreXcs + 15

	resp, err := cosmosclient.BroadcastTx(conf.ctx, broadcaster, &conf.OriginChainTxSigner, msgTransfer)
	if err != nil {
		return err
	}
	if resp.GasUsed == 0 || resp.GasWanted == 0 || resp.Code != 0 || resp.TxHash == "" {
		return fmt.Errorf("invalid MsgTransfer with hash %s and TX code %d", resp.TxHash, resp.Code)
	}

	// Wait for 15 blocks (relaying XCS can be slow on mainnet)
	err = testutil.WaitForCondition(time.Second*120, time.Second*5, func() (bool, error) {
		height, err := conf.OriginChainClient.QueryLatestHeight(conf.ctx)
		if err != nil {
			return false, nil
		}
		return height >= desiredHeight, nil
	})

	if err != nil {
		return err
	}

	// Check the final balance of the input token after the crosschain swap.
	originTokenInFinalBalance, err := query.Bank_Balance(stakingAddress, conf.OriginChainTokenInDenom)
	if err != nil {
		return err
	}

	expectedFinalBalance := originInputTokenInitialBalance.Balance.Amount.Sub(swapAmount)
	// Since the swap address pays fees, the staking address balance should be exactly as expected
	if !expectedFinalBalance.Equal(originTokenInFinalBalance.Balance.Amount) {
		conf.logger.Warn(
			"unexpected input token balance",
			zap.String("chain", conf.OriginChainName),
			zap.String("token_in_denom", conf.OriginChainTokenInDenom),
			zap.String("expected", expectedFinalBalance.String()),
			zap.String("actual", originTokenInFinalBalance.Balance.Amount.String()),
		)
	}

	conf.logger.Debug("input token final balance", zap.String("chain", conf.OriginChainName), zap.String("Coin", originTokenInFinalBalance.Balance.String()))

	// Check the current balance of the output token prior to the crosschain swap.
	outputTokenFinalBalance, err := outputTokenBalanceQuery.Bank_Balance(destinationAddress, outputDenom)
	if err != nil {
		return err
	}

	if !outputTokenFinalBalance.Balance.Amount.GT(outputTokenInitialBalance.Balance.Amount) {
		conf.logger.Warn(
			"unexpected output token balance",
			zap.String("chain", conf.OriginChainName),
			zap.String("token_out_denom", outputTokenFinalBalance.Balance.Denom),
			zap.String("balance initial", outputTokenInitialBalance.String()),
			zap.String("balance final", outputTokenFinalBalance.String()),
		)
	}
}

// Performs an IBC transfer via IBC hooks to invoke the XCSv2 contract on Osmosis.
// Funds are swapped, then either left on Osmosis or transferred back to the origin chain via IBC.
func CrosschainSwap(conf *Xcsv2OriginChainConfig, osmosisConfig *Xcsv2OsmosisConfig) error {
	// The token denom and destination, which can be either Osmosis or the origin, depending on configuration.
	destinationCc, destinationAddress, outputDenom := getOutputTokenDestinationChainParams(conf, osmosisConfig)

	// At present, we submit each swap request individually (per address)
	for _, stakingAddress := range conf.StakingAddresses {

	}

	return nil
}
