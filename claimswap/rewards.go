package claimswap

import (
	"context"
	"errors"

	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"

	"github.com/KyleMoser/Cronmos/helpers"
	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	disttypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
)

func GetUnclaimedValidatorCommission(conf *Xcsv2OriginChainConfig, osmosisConfig *Xcsv2OsmosisConfig) (
	balances map[string]sdk.Coin,
	unclaimedCommission map[string]sdkmath.Int,
	err error,
) {
	broadcaster := cosmosclient.NewBroadcaster(conf.OriginChainClient)
	queryCtx, err := broadcaster.GetClientContext(context.Background(), &conf.OriginChainTxSigner)
	if err != nil {
		return nil, nil, err
	}
	distClient := disttypes.NewQueryClient(queryCtx)
	bankClient := banktypes.NewQueryClient(queryCtx)
	valAddr, err := sdk.ValAddressFromBech32(conf.ValidatorAddress)
	if err != nil {
		return nil, nil, err
	}
	valAddrB := valAddr.Bytes()
	accAddr := sdk.AccAddress(valAddrB)
	if err != nil {
		return nil, nil, err
	}

	balances, unclaimedCommission, err = helpers.UnclaimedValidatorCommissions(distClient, bankClient, context.Background(), conf.ValidatorAddress, accAddr)
	return
}

func ClaimValidatorCommission(
	conf *Xcsv2OriginChainConfig,
	osmosisConfig *Xcsv2OsmosisConfig,
	validatorBalances map[string]sdk.Coin,
	expectedCommissionMap map[string]sdkmath.Int,
) (claimedCommissionMap map[string]sdkmath.Int, err error) {
	broadcaster := cosmosclient.NewBroadcaster(conf.OriginChainClient)
	queryCtx, err := broadcaster.GetClientContext(context.Background(), &conf.OriginChainTxSigner)
	if err != nil {
		return nil, err
	}
	bankClient := banktypes.NewQueryClient(queryCtx)
	valAddr, err := sdk.ValAddressFromBech32(conf.ValidatorAddress)
	if err != nil {
		return nil, err
	}
	valAddrB := valAddr.Bytes()
	accAddr := sdk.AccAddress(valAddrB)
	accBech32, err := bech32.ConvertAndEncode(conf.OriginChainClient.Config.AccountPrefix, valAddrB)
	if err != nil {
		return nil, err
	}

	if len(expectedCommissionMap) == 0 {
		return nil, errors.New("No claimable validator commission for " + conf.ValidatorAddress)
	}

	hash, err := helpers.ClaimValidatorCommission(conf.OriginChainTxSignerAddress, conf.ValidatorAddress, &conf.OriginChainTxSigner, conf.OriginChainClient)
	if err != nil {
		return nil, err
	}

	claimTx, err := helpers.GetTx(queryCtx, hash)
	if err != nil {
		return nil, err
	}

	isClaimed, err := helpers.ValidatePostClaimBalances(queryCtx.Codec, bankClient, accAddr, accBech32, claimTx, validatorBalances, expectedCommissionMap)
	if err != nil {
		return nil, err
	} else if !isClaimed {
		return nil, errors.New("Unexpected error claiming validator commission")
	}

	return expectedCommissionMap, nil
}

func ClaimDelegatorRewards(
	originChainClient *cosmosclient.ChainClient,
	originChainTxSigner helpers.CosmosUser,
	grantee string,
	delegator string,
	valoperAddress string,
	chainBech32AccountPrefix string,
	conf *Xcsv2OriginChainConfig,
	osmosisConfig *Xcsv2OsmosisConfig,
	preClaimBalances map[string]sdk.Coin,
	unclaimedCommission map[string]sdkmath.Int,
) (claimedCommissionMap map[string]sdkmath.Int, err error) {
	broadcaster := cosmosclient.NewBroadcaster(originChainClient)
	queryCtx, err := broadcaster.GetClientContext(context.Background(), &originChainTxSigner)
	if err != nil {
		return nil, err
	}
	bankClient := banktypes.NewQueryClient(queryCtx)
	valAddr, err := sdk.ValAddressFromBech32(valoperAddress)
	if err != nil {
		return nil, err
	}
	valAddrB := valAddr.Bytes()
	validatorAccAddr := sdk.AccAddress(valAddrB)
	validatorAccBech32, err := bech32.ConvertAndEncode(chainBech32AccountPrefix, valAddrB)
	if err != nil {
		return nil, err
	}

	hash, err := helpers.ClaimDelegatorRewards(grantee, delegator, valoperAddress, &originChainTxSigner, originChainClient)
	if err != nil {
		return nil, err
	}

	claimTx, err := helpers.GetTx(queryCtx, hash)
	if err != nil {
		return nil, err
	}

	isClaimed, err := helpers.ValidatePostClaimBalances(queryCtx.Codec, bankClient, validatorAccAddr, validatorAccBech32, claimTx, preClaimBalances, unclaimedCommission)
	if err != nil {
		return nil, err
	} else if !isClaimed {
		return nil, errors.New("Unexpected error claiming delegator rewards")
	}

	return unclaimedCommission, nil
}

func GetUnclaimedDelegatorRewards(
	originChainClient *cosmosclient.ChainClient,
	originChainTxSigner *helpers.CosmosUser,
	delegator string,
	valoperAddress string,
	withdrawalAddress sdk.AccAddress,
) (balances map[string]sdk.Coin, unclaimedCommission map[string]sdkmath.Int, err error) {
	broadcaster := cosmosclient.NewBroadcaster(originChainClient)
	queryCtx, err := broadcaster.GetClientContext(context.Background(), originChainTxSigner)
	if err != nil {
		return nil, nil, err
	}
	distClient := disttypes.NewQueryClient(queryCtx)
	bankClient := banktypes.NewQueryClient(queryCtx)
	balances, unclaimedCommission, err = helpers.UnclaimedDelegatorRewards(distClient, bankClient, context.Background(), delegator, valoperAddress, withdrawalAddress)
	return
}
