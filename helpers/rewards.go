package helpers

import (
	"context"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"

	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	ctypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	disttypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
)

var (
	withdrawCommission = "/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission"
	withdrawReward     = "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward"
)

func ValidatePostClaimBalances(
	cdc codec.Codec,
	bankClient banktypes.QueryClient,
	accAddr sdk.AccAddress,
	accAddrBech32 string,
	claimTx *txTypes.Tx,
	balancesBeforeClaim map[string]sdk.Coin,
	expectedCommisionMap map[string]sdkmath.Int) (bool, error) {
	// Check that the new balance is greater than the previous balance
	for denom, balanceBefore := range balancesBeforeClaim {
		queryBalReq := banktypes.NewQueryBalanceRequest(accAddr, denom)
		res, err := bankClient.Balance(context.Background(), queryBalReq)
		if err != nil {
			return false, err
		}

		balanceAfter := res.Balance.Amount
		feePaid := claimTx.GetFee()
		isFeeDenom, feeCoin := feePaid.Find(denom)
		expectedCommission := expectedCommisionMap[denom]
		feePayerAddr := claimTx.FeePayer(cdc)
		fpa := sdk.AccAddress{}
		err = fpa.Unmarshal(feePayerAddr)
		if err != nil {
			return false, err
		}

		changeInBalance := balanceAfter.Sub(balanceBefore.Amount)
		if accAddr.Equals(fpa) && isFeeDenom {
			expectedCommission = expectedCommission.Sub(feeCoin.Amount)
		}

		if !changeInBalance.GTE(expectedCommission) && accAddr.Equals(fpa) && isFeeDenom {
			return false, fmt.Errorf("account %s, balance before commission %s, after commission %s, TX fees paid: %s, commission: %s", accAddrBech32, balanceBefore.String(), res.Balance.String(), feeCoin.String(), expectedCommission.String())
		} else if !changeInBalance.GTE(expectedCommission) {
			return false, fmt.Errorf("account %s, balance before commission %s, after commission %s, commission: %s", accAddrBech32, balanceBefore.String(), res.Balance.String(), expectedCommission.String())
		}
	}

	return true, nil
}

func UnclaimedDelegatorRewards(
	distClient disttypes.QueryClient,
	bankClient banktypes.QueryClient,
	ctx context.Context,
	delegatorAddress string,
	validatorAddress string,
	valAddr sdk.AccAddress,
) (
	balancesBeforeClaim map[string]sdk.Coin,
	expectedRewardsMap map[string]sdkmath.Int,
	err error,
) {
	expectedRewardsMap = map[string]sdkmath.Int{}
	balancesBeforeClaim = map[string]sdk.Coin{}

	delegatorRewardsResp, err := distClient.DelegationRewards(ctx, &disttypes.QueryDelegationRewardsRequest{
		DelegatorAddress: delegatorAddress,
		ValidatorAddress: validatorAddress,
	})

	if err != nil {
		return nil, nil, err
	}
	unclaimedRewardsCoins := delegatorRewardsResp.GetRewards()

	// Check the current bank balance for the delegator, for the denoms we can claim as commission
	for _, unclaimedRewardsCoin := range unclaimedRewardsCoins {
		queryBalReq := banktypes.NewQueryBalanceRequest(valAddr, unclaimedRewardsCoin.Denom)
		res, err := bankClient.Balance(ctx, queryBalReq)
		if err != nil {
			return nil, nil, err
		}
		balancesBeforeClaim[unclaimedRewardsCoin.Denom] = *res.Balance
		expectedRewardsMap[unclaimedRewardsCoin.Denom] = unclaimedRewardsCoin.Amount.TruncateInt()
	}

	return
}

func UnclaimedValidatorCommissions(
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

func ClaimValidatorCommission(grantee, valAddr string, signer *CosmosUser, chainClient *cosmosclient.ChainClient) error {
	originHeightPreXcs, err := chainClient.QueryLatestHeight(context.Background())
	if err != nil {
		return err
	}
	desiredHeight := originHeightPreXcs + 2

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
	broadcaster := cosmosclient.NewBroadcaster(chainClient)

	resp, err := cosmosclient.BroadcastTx(ctx, broadcaster, signer, authzMsgClaim)
	if err != nil {
		return err
	}
	if resp.GasUsed == 0 || resp.GasWanted == 0 || resp.Code != 0 || resp.TxHash == "" {
		return fmt.Errorf("invalid MsgExec (%s) with hash %s and TX code %d", withdrawCommission, resp.TxHash, resp.Code)
	}

	// Wait for 2 blocks
	err = testutil.WaitForCondition(time.Second*14, time.Second*6, func() (bool, error) {
		height, err := chainClient.QueryLatestHeight(context.Background())
		if err != nil {
			return false, nil
		}
		return height >= desiredHeight, nil
	})

	return err
}
