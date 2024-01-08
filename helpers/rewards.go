package helpers

import (
	"context"
	"fmt"
	"slices"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/KyleMoser/cosmos-client/client"
	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	"github.com/cosmos/cosmos-sdk/codec"
	ctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	pager "github.com/cosmos/cosmos-sdk/types/query"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	disttypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/strangelove-ventures/interchaintest/v8/testutil"
)

var (
	withdrawCommission = "/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission"
	withdrawReward     = "/cosmos.distribution.v1beta1.MsgWithdrawDelegatorReward"
	sendAuthorization  = "/cosmos.bank.v1beta1.SendAuthorization"
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

func GetPreSwapSend(
	sendAuthorizationGranter string,
	sendAuthorizationGrantee string,
	signer *CosmosUser,
	chainClient *client.ChainClient,
	claimedRewards sdk.Coins) (*banktypes.MsgSend, error) {
	var preSwapSend *banktypes.MsgSend

	coins := sdk.Coins{}
	allowedSpendMap, err := GetGranteeSpendLimit(sendAuthorizationGranter, sendAuthorizationGrantee, signer, chainClient)
	if err != nil {
		return nil, err
	}

	for _, coin := range claimedRewards {
		denom := coin.Denom
		claimedRewardAmount := coin.Amount

		if allowedAmount, ok := allowedSpendMap[denom]; ok {
			amountFinal := claimedRewardAmount
			if allowedAmount.LT(amountFinal) {
				amountFinal = allowedAmount
			}

			coins = append(coins, sdk.NewCoin(denom, amountFinal))
		}
	}

	bz, err := sdk.GetFromBech32(sendAuthorizationGranter, chainClient.Config.AccountPrefix)
	if err != nil {
		return nil, err
	}

	granterAccAddr := sdk.AccAddress(bz)

	bz2, err := sdk.GetFromBech32(sendAuthorizationGrantee, chainClient.Config.AccountPrefix)
	if err != nil {
		return nil, err
	}

	granteeAccAddr := sdk.AccAddress(bz2)
	preSwapSend = banktypes.NewMsgSend(granterAccAddr, granteeAccAddr, coins)
	return preSwapSend, nil
}

func UnclaimedDelegatorRewards(
	distClient disttypes.QueryClient,
	bankClient banktypes.QueryClient,
	ctx context.Context,
	delegatorAddress string,
	validatorAddress string,
	withdrawalAddress sdk.AccAddress,
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

	// Check the current bank balance for the delegator's withdrawal address, for the denoms we can claim as commission
	for _, unclaimedRewardsCoin := range unclaimedRewardsCoins {
		queryBalReq := banktypes.NewQueryBalanceRequest(withdrawalAddress, unclaimedRewardsCoin.Denom)
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
		if unclaimedCommissionCoin.Amount.TruncateInt().GT(sdkmath.ZeroInt()) {
			queryBalReq := banktypes.NewQueryBalanceRequest(valAddr, unclaimedCommissionCoin.Denom)
			res, err := bankClient.Balance(ctx, queryBalReq)
			if err != nil {
				return nil, nil, err
			}
			balancesBeforeClaim[unclaimedCommissionCoin.Denom] = *res.Balance
			expectedCommisionMap[unclaimedCommissionCoin.Denom] = unclaimedCommissionCoin.Amount.TruncateInt()
		}
	}

	return
}

func IsAuthorizedClaimValidatorCommission(grantee, valAddr string, signer *CosmosUser, chainClient *cosmosclient.ChainClient) (bool, error) {
	broadcaster := cosmosclient.NewBroadcaster(chainClient)
	queryCtx, err := broadcaster.GetClientContext(context.Background(), signer)
	if err != nil {
		return false, err
	}
	authzClient := authz.NewQueryClient(queryCtx)
	validGrants, err := getValidGrants(authzClient, valAddr, grantee, withdrawCommission, nil, chainClient)
	return len(validGrants) > 0, err
}

func IsAuthorizedClaimDelegatorRewards(grantee, delegatorAddr string, signer *CosmosUser, chainClient *cosmosclient.ChainClient) (bool, error) {
	broadcaster := cosmosclient.NewBroadcaster(chainClient)
	queryCtx, err := broadcaster.GetClientContext(context.Background(), signer)
	if err != nil {
		return false, err
	}
	authzClient := authz.NewQueryClient(queryCtx)
	validGrants, err := getValidGrants(authzClient, delegatorAddr, grantee, withdrawReward, nil, chainClient)
	return len(validGrants) > 0, err
}

func GetDelegatorRewardsWithdrawalAddress(delegatorAddr string, signer *CosmosUser, chainClient *cosmosclient.ChainClient) (string, error) {
	broadcaster := cosmosclient.NewBroadcaster(chainClient)
	queryCtx, err := broadcaster.GetClientContext(context.Background(), signer)
	if err != nil {
		return "", err
	}
	distClient := disttypes.NewQueryClient(queryCtx)
	resp, err := distClient.DelegatorWithdrawAddress(context.Background(), &disttypes.QueryDelegatorWithdrawAddressRequest{DelegatorAddress: delegatorAddr})
	if err != nil {
		return "", err
	}

	return resp.WithdrawAddress, nil
}

func GetGranteeSpendLimit(
	granter string,
	grantee string,
	signer *CosmosUser,
	chainClient *cosmosclient.ChainClient) (spendLimitMap map[string]sdkmath.Int, err error) {
	broadcaster := cosmosclient.NewBroadcaster(chainClient)
	queryCtx, err := broadcaster.GetClientContext(context.Background(), signer)
	if err != nil {
		return nil, err
	}
	authzClient := authz.NewQueryClient(queryCtx)

	allSendGrants, err := getValidGrants(authzClient, granter, grantee, sendAuthorization, nil, chainClient)
	if err != nil {
		return nil, err
	}

	spendLimitMap = map[string]sdkmath.Int{}
	for _, grant := range allSendGrants {
		var bankAuthorization banktypes.SendAuthorization
		e := chainClient.Codec.InterfaceRegistry.UnpackAny(grant.Authorization, &bankAuthorization)
		if e != nil {
			return nil, e
		}

		if len(bankAuthorization.AllowList) == 0 || slices.Contains[[]string](bankAuthorization.AllowList, grantee) {
			for _, token := range bankAuthorization.SpendLimit {
				if _, ok := spendLimitMap[token.Denom]; ok {
					spendLimitMap[token.Denom] = spendLimitMap[token.Denom].Add(token.Amount)
				} else {
					spendLimitMap[token.Denom] = token.Amount
				}
			}
		}
	}
	return spendLimitMap, nil
}

func getValidGrants(
	authzClient authz.QueryClient,
	granter string,
	grantee string,
	msgTypeUrl string,
	paginator *pager.PageRequest,
	chainClient *cosmosclient.ChainClient) ([]*authz.Grant, error) {
	grants := []*authz.Grant{}
	allPages := paginator == nil

	req := &authz.QueryGrantsRequest{
		Granter:    granter,
		Grantee:    grantee,
		MsgTypeUrl: msgTypeUrl,
	}

	hasNextPage := true

	for {
		resp, err := authzClient.Grants(context.Background(), req)
		if err != nil {
			return nil, err
		}

		if resp.Grants != nil {
			for _, grant := range resp.Grants {
				if grant.Authorization.TypeUrl == msgTypeUrl {
					if grant.Expiration == nil { // never expires
						grants = append(grants, grant)
					} else if time.Now().Before(*grant.Expiration) {
						grants = append(grants, grant) // is not expired
					}
				}
			}
		}

		if resp.Pagination != nil && resp.Pagination.NextKey != nil {
			req.Pagination.Key = resp.Pagination.NextKey
			if len(resp.Pagination.NextKey) == 0 {
				hasNextPage = false
			}
		} else {
			hasNextPage = false
		}

		if !allPages || !hasNextPage {
			break
		}
	}

	return grants, nil
}

func ClaimValidatorCommission(grantee, valAddr string, signer *CosmosUser, chainClient *cosmosclient.ChainClient) (txHash string, err error) {
	originHeightPreXcs, err := chainClient.QueryLatestHeight(context.Background())
	if err != nil {
		return "", err
	}
	desiredHeight := originHeightPreXcs + 2

	claimMsg := disttypes.NewMsgWithdrawValidatorCommission(valAddr)
	claimMsgBytes, err := claimMsg.Marshal()
	if err != nil {
		return "", err
	}

	authzMsgClaim := &authz.MsgExec{
		Grantee: grantee,
		Msgs:    []*ctypes.Any{{TypeUrl: withdrawCommission, Value: claimMsgBytes}},
	}

	ctx := context.Background()
	broadcaster := cosmosclient.NewBroadcaster(chainClient)

	resp, err := cosmosclient.BroadcastTx(ctx, broadcaster, signer, authzMsgClaim)
	if err != nil {
		return "", err
	}
	if resp.GasUsed == 0 || resp.GasWanted == 0 || resp.Code != 0 || resp.TxHash == "" {
		return "", fmt.Errorf("invalid MsgExec (%s) with hash %s and TX code %d", withdrawCommission, resp.TxHash, resp.Code)
	}

	// Wait for 2 blocks
	err = testutil.WaitForCondition(time.Second*14, time.Second*6, func() (bool, error) {
		height, err := chainClient.QueryLatestHeight(context.Background())
		if err != nil {
			return false, nil
		}
		return height >= desiredHeight, nil
	})

	return resp.TxHash, err
}

func ClaimDelegatorRewards(grantee, delAddr, valAddr string, signer *CosmosUser, chainClient *cosmosclient.ChainClient) (txHash string, err error) {
	originHeightPreXcs, err := chainClient.QueryLatestHeight(context.Background())
	if err != nil {
		return "", err
	}
	desiredHeight := originHeightPreXcs + 2

	claimMsg := disttypes.NewMsgWithdrawDelegatorReward(delAddr, valAddr)
	claimMsgBytes, err := claimMsg.Marshal()
	if err != nil {
		return "", err
	}

	authzMsgClaim := &authz.MsgExec{
		Grantee: grantee,
		Msgs:    []*ctypes.Any{{TypeUrl: withdrawReward, Value: claimMsgBytes}},
	}

	ctx := context.Background()
	broadcaster := cosmosclient.NewBroadcaster(chainClient)

	resp, err := cosmosclient.BroadcastTx(ctx, broadcaster, signer, authzMsgClaim)
	if err != nil {
		return "", err
	}
	if resp.GasUsed == 0 || resp.GasWanted == 0 || resp.Code != 0 || resp.TxHash == "" {
		return "", fmt.Errorf("invalid MsgExec (%s) with hash %s and TX code %d", withdrawCommission, resp.TxHash, resp.Code)
	}

	// Wait for 2 blocks
	err = testutil.WaitForCondition(time.Second*14, time.Second*6, func() (bool, error) {
		height, err := chainClient.QueryLatestHeight(context.Background())
		if err != nil {
			return false, nil
		}
		return height >= desiredHeight, nil
	})

	return resp.TxHash, err
}
