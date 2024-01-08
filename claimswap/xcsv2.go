package claimswap

import (
	"context"
	"fmt"
	"time"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	sdkmath "cosmossdk.io/math"
	"github.com/KyleMoser/Cronmos/helpers"
	"github.com/KyleMoser/Cronmos/wasm"
	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	"github.com/KyleMoser/cosmos-client/client/query"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
	"go.uber.org/zap"
)

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
	optionalPreSwapSend *banktypes.MsgSend,
	ctx context.Context,
	logger zap.Logger,
	chainClient *cosmosclient.ChainClient,
	xcsSwapAmount sdkmath.Int,
	xcsTxSigner cosmosclient.User,
	srcChannel,
	srcPort,
	clientId,
	xcsRecoveryAddress,
	crosschainSwapDestAddr,
	xcsOutputDenomOsmosis,
	destChainInputDenom,
	destChainOutputDenom string) (tokensOut sdkmath.Int, err error) {
	zi := sdkmath.ZeroInt()
	done := chainClient.SetSDKContext()
	defer done()
	txSignerAddr := xcsTxSigner.FormattedAddress()

	destChainOutputDenomInitialBalance, err := getBalance(chainClient, crosschainSwapDestAddr, destChainOutputDenom)
	if err != nil {
		return zi, err
	}

	destChainInputDenomInitialBalance, err := getBalance(chainClient, txSignerAddr, destChainInputDenom)
	if err != nil {
		return zi, err
	}

	originHeight, err := chainClient.QueryLatestHeight(ctx)
	if err != nil {
		return zi, err
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
	xcsWasmMemo, err := wasm.CrosschainSwapMemo(xcsRecoveryAddress, crosschainSwapDestAddr, xcsOutputDenomOsmosis, osmosisSwapForwardContract)
	if err != nil {
		return zi, err
	}

	// Create IBC Transfer message with channel, port, and client ID from IBC chain registry.
	// See https://github.com/cosmos/chain-registry/blob/master/_IBC/cosmoshub-osmosis.json.
	// TODO: move these params out of this configuration and look it up automatically.
	msgTransfer, err := helpers.PrepareIbcTransfer(
		chainClient,
		originHeight,
		srcChannel,
		srcPort,
		clientId)
	if err != nil {
		return zi, err
	}

	msgTransfer.Sender = xcsTxSigner.FormattedAddress()
	msgTransfer.Receiver = osmosisSwapForwardContract
	msgTransfer.Token = sdk.NewCoin(destChainInputDenom, xcsSwapAmount)
	msgTransfer.Memo = xcsWasmMemo // this will invoke IBC hooks on the recipient chain

	broadcaster := cosmosclient.NewBroadcaster(chainClient)

	originHeightPreXcs, err := chainClient.QueryLatestHeight(ctx)
	if err != nil {
		return zi, err
	}
	desiredHeight := originHeightPreXcs + 15

	msgs := []sdk.Msg{}
	if optionalPreSwapSend != nil {
		msgs = append(msgs, optionalPreSwapSend)
	}
	msgs = append(msgs, msgTransfer)

	resp, err := cosmosclient.BroadcastTx(ctx, broadcaster, xcsTxSigner, msgs...)
	if err != nil {
		return zi, err
	}
	if resp.GasUsed == 0 || resp.GasWanted == 0 || resp.Code != 0 || resp.TxHash == "" {
		return zi, fmt.Errorf("invalid MsgTransfer with hash %s and TX code %d", resp.TxHash, resp.Code)
	}

	// Wait for 15 blocks (relaying XCS can be slow on mainnet)
	err = testutil.WaitForCondition(time.Second*120, time.Second*5, func() (bool, error) {
		height, err := chainClient.QueryLatestHeight(ctx)
		if err != nil {
			return false, nil
		}
		return height >= desiredHeight, nil
	})

	if err != nil {
		return zi, err
	}

	destChainInputDenomFinalBalance, err := getBalance(chainClient, txSignerAddr, destChainInputDenom)
	if err != nil {
		return zi, err
	}

	// Subtract fees paid for the TX from the expected final balance of the input swap token
	expectedFinalBalance := destChainInputDenomInitialBalance.Sub(xcsSwapAmount)
	queryCtx, err := broadcaster.GetClientContext(ctx, xcsTxSigner)
	if err != nil {
		return zi, err
	}
	feeTx, err := helpers.GetTx(queryCtx, resp.TxHash)
	if err != nil {
		return zi, err
	}
	feePayerAccAddr := feeTx.FeePayer(queryCtx.Codec)
	fpa := sdk.AccAddress{}
	err = fpa.Unmarshal(feePayerAccAddr)
	if err != nil {
		return zi, err
	}
	feePaid := feeTx.GetFee()
	feePayerAddr := fpa.String()
	isFeeDenom, feeCoin := feePaid.Find(destChainInputDenom)
	if txSignerAddr == feePayerAddr && isFeeDenom {
		expectedFinalBalance = expectedFinalBalance.Sub(feeCoin.Amount)
	}

	// Since the signer pays fees, the staking address balance should be exactly as expected
	if !expectedFinalBalance.LTE(destChainInputDenomFinalBalance) {
		logger.Warn(
			"unexpected input token balance",
			zap.String("chain", chainClient.Config.ChainName),
			zap.String("signer address", txSignerAddr),
			zap.String("token_in_denom", destChainInputDenom),
			zap.String("expected", expectedFinalBalance.String()),
			zap.String("actual", destChainInputDenomFinalBalance.String()),
		)
	} else {
		logger.Debug(
			"input token final balance",
			zap.String("chain", chainClient.Config.ChainName),
			zap.String("Coin", destChainInputDenomFinalBalance.String()))
	}

	// Check the current balance of the output token after the crosschain swap.
	destChainOutputDenomFinalBalance, err := getBalance(chainClient, crosschainSwapDestAddr, destChainOutputDenom)
	if err != nil {
		return zi, err
	}

	outputDenomDiff := destChainOutputDenomFinalBalance.Sub(destChainOutputDenomInitialBalance)

	if outputDenomDiff.LTE(sdkmath.ZeroInt()) {
		logger.Warn(
			"unexpected output token balance",
			zap.String("chain", chainClient.Config.ChainName),
			zap.String("token_out_denom", destChainOutputDenom),
			zap.String("balance initial", destChainOutputDenomInitialBalance.String()),
			zap.String("balance final", destChainOutputDenomFinalBalance.String()),
		)
	}

	return outputDenomDiff, nil
}

// Performs an IBC transfer via IBC hooks to invoke the XCSv2 contract on Osmosis.
// Funds are swapped, then either left on Osmosis or transferred back to the origin chain via IBC.
func ValidatorCommissionCrosschainSwap(
	preSwapSend *banktypes.MsgSend,
	conf *Xcsv2OriginChainConfig,
	osmosisConfig *Xcsv2OsmosisConfig,
	swapCoins sdk.Coins,
) error {
	for _, coin := range swapCoins {
		//Do not allow a trade above the configured maximum
		xcsSwapAmount := coin.Amount
		if xcsSwapAmount.GT(conf.OriginChainTokenInMax) {
			xcsSwapAmount = conf.OriginChainTokenInMax
		}

		if coin.Denom == conf.OriginChainTokenInDenom {
			tokensOut, err := crosschainSwap(
				preSwapSend,
				conf.ctx,
				*conf.logger,
				conf.OriginChainClient,
				xcsSwapAmount,
				&conf.OriginChainTxSigner,
				conf.OriginToOsmosisSrcChannel,
				conf.OriginToOsmosisSrcPort,
				conf.OriginToOsmosisClientId,
				conf.OsmosisXcsv2RecoveryAddress,
				osmosisConfig.DestinationAddress,
				conf.OsmosisOutputDenom,
				coin.Denom,
				conf.OriginChainSwapOutputDenom,
			)

			if err != nil {
				conf.logger.Error("validator commission XCSv2 error", zap.Error(err))
			} else {
				conf.logger.Info("validator commission XCSv2 swap",
					zap.String("Validator address", conf.ValidatorAddress),
					zap.String("commission collected", coin.String()),
					zap.String("XCSv2 tokens in", xcsSwapAmount.String()),
					zap.String("XCSv2 tokens out", tokensOut.String()),
				)
			}
		} else {
			conf.logger.Warn("app misconfiguration",
				zap.String("configured swap denom", conf.OriginChainTokenInDenom),
				zap.String("validator commission denom", coin.Denom),
			)
		}
	}

	return nil
}

// Performs an IBC transfer via IBC hooks to invoke the XCSv2 contract on Osmosis.
// Funds are swapped, then either left on Osmosis or transferred back to the origin chain via IBC.
func DelegatorRewardsCrosschainSwap(
	optionalPreSwapSend *banktypes.MsgSend,
	conf *Xcsv2OriginChainConfig,
	osmosisConfig *Xcsv2OsmosisConfig,
	swapCoins sdk.Coins,
) error {
	for _, coin := range swapCoins {
		rewardAmount := coin.Amount
		rewardDenom := coin.Denom

		//Do not allow a trade above the configured maximum
		xcsSwapAmount := rewardAmount
		if xcsSwapAmount.GT(conf.OriginChainTokenInMax) {
			xcsSwapAmount = conf.OriginChainTokenInMax
		}

		if rewardDenom == conf.OriginChainTokenInDenom {
			tokensOut, err := crosschainSwap(
				optionalPreSwapSend,
				conf.ctx,
				*conf.logger,
				conf.OriginChainClient,
				xcsSwapAmount,
				&conf.OriginChainTxSigner,
				conf.OriginToOsmosisSrcChannel,
				conf.OriginToOsmosisSrcPort,
				conf.OriginToOsmosisClientId,
				conf.OsmosisXcsv2RecoveryAddress,
				osmosisConfig.DestinationAddress,
				conf.OsmosisOutputDenom,
				rewardDenom,
				conf.OriginChainSwapOutputDenom,
			)

			if err != nil {
				conf.logger.Error("validator commission XCSv2 error", zap.Error(err))
			} else {
				conf.logger.Info("validator commission XCSv2 swap",
					zap.String("Validator address", conf.ValidatorAddress),
					zap.String("commission collected", rewardDenom+rewardAmount.String()),
					zap.String("XCSv2 tokens in", xcsSwapAmount.String()),
					zap.String("XCSv2 tokens out", tokensOut.String()),
				)
			}
		} else {
			conf.logger.Warn("app misconfiguration",
				zap.String("configured swap denom", conf.OriginChainTokenInDenom),
				zap.String("validator commission denom", rewardDenom),
			)
		}
	}

	return nil
}
