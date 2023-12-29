package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/KyleMoser/Cronmos/claimswap"
	"github.com/KyleMoser/Cronmos/helpers"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var config string

func main() {
	sdk.SetAddrCacheEnabled(false)

	flag.StringVar(&config, "config", "", "path to config for the application")
	flag.Parse()

	conf, err := helpers.ReadYamlConfig(config)
	if err != nil {
		log.Fatal(err.Error())
	}

	xcsv2Config, osmoConfig, err := claimswap.ToXcsv2Config(conf)
	if err != nil {
		log.Fatal(err.Error())
	}

	for _, chain := range xcsv2Config {
		balancesBeforeClaim, expectedCommissionMap, err := claimswap.GetUnclaimedValidatorCommission(chain, osmoConfig)
		if err != nil {
			log.Fatal(err.Error())
		}

		for _, coin := range balancesBeforeClaim {
			log.Printf("Validator %s balance: %s\n", chain.ValidatorAddress, coin)
		}
		for denom, amount := range expectedCommissionMap {
			log.Printf("Validator %s unclaimed commission: %s%s\n", chain.ValidatorAddress, denom, amount.String())
		}

		authorizedGrantee, err := helpers.IsAuthorizedClaimValidatorCommission(chain.OriginChainTxSignerAddress, chain.ValidatorAddress, &chain.OriginChainTxSigner, chain.OriginChainClient)
		if err != nil {
			log.Fatal(err.Error())
		} else if !authorizedGrantee {
			log.Printf("'%s' is not authorized to claim validator commission for '%s'\n", chain.OriginChainTxSignerAddress, chain.ValidatorAddress)
		} else {
			claimedCommissionMap, err := claimswap.ClaimValidatorCommission(chain, osmoConfig, balancesBeforeClaim, expectedCommissionMap)
			if err != nil {
				log.Fatal(err.Error())
			} else if len(claimedCommissionMap) == 0 {
				fmt.Printf("No rewards claimed for chain " + chain.OriginChainName)
			} else {
				err = claimswap.ValidatorCommissionCrosschainSwap(chain, osmoConfig, claimedCommissionMap)
				if err != nil {
					log.Fatal(err.Error())
				}
			}
		}

		for _, delegator := range chain.DelegatorAddresses {
			withdrawAddr, err := helpers.GetDelegatorRewardsWithdrawalAddress(delegator, &chain.OriginChainTxSigner, chain.OriginChainClient)
			if err != nil {
				log.Fatal(err.Error())
			}

			bz, err := sdk.GetFromBech32(withdrawAddr, chain.OriginChainClient.Config.AccountPrefix)
			if err != nil {
				log.Fatal(err.Error())
			}
			withdrawalAccAddr := sdk.AccAddress(bz)

			preClaimDelegatorBalances, unclaimedDelegatorRewards, err := claimswap.GetUnclaimedDelegatorRewards(chain.OriginChainClient, &chain.OriginChainTxSigner, delegator, chain.ValidatorAddress, withdrawalAccAddr)
			if err != nil {
				log.Fatal(err.Error())
			}

			if len(unclaimedDelegatorRewards) == 0 {
				log.Printf("No unclaimed delegator rewards for delegator %s and validator %s\n", delegator, chain.ValidatorAddress)
			} else {
				for _, coin := range preClaimDelegatorBalances {
					log.Printf("delegator withdrawal address: %s, validator: %s, pre-claim balance: %s\n", withdrawAddr, chain.ValidatorAddress, coin)
				}
				for denom, amount := range unclaimedDelegatorRewards {
					log.Printf("delegator withdrawal address: %s, validator: %s, unclaimed reward: %s%s\n", withdrawAddr, chain.ValidatorAddress, denom, amount.String())
				}
				claimedDelRewards, err := claimswap.ClaimDelegatorRewards(chain.OriginChainClient, chain.OriginChainTxSigner, chain.OriginChainTxSignerAddress, delegator, chain.ValidatorAddress, chain.OriginChainClientConfig.AccountPrefix, chain, osmoConfig, preClaimDelegatorBalances, unclaimedDelegatorRewards)

				if err != nil {
					log.Fatal(err.Error())
				} else if len(claimedDelRewards) == 0 {
					fmt.Printf("No delegator rewards claimed for chain " + chain.OriginChainName)
				} else {
					// Do the crosschain swap
				}
			}

		}
	}
}
