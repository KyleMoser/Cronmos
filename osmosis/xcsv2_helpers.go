package osmosis

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/KyleMoser/Cronmos/helpers"
	"github.com/KyleMoser/Cronmos/logging"
	"github.com/KyleMoser/Cronmos/wasm"
	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	registry "github.com/KyleMoser/cosmos-client/client/chain_registry"
	"github.com/KyleMoser/cosmos-client/cmd"
	"go.uber.org/zap"
)

func ToXcsv2Config(conf *helpers.Config) ([]*Xcsv2OriginChainConfig, *Xcsv2OsmosisConfig, error) {
	swapConfigs := []*Xcsv2OriginChainConfig{}
	logger := logging.DoConfigureLogger("DEBUG")
	ctx := context.Background()

	osmosisConfig := &Xcsv2OsmosisConfig{
		HomeDir:         conf.Xcsv2Config.OsmosisHome,
		TxSignerAddress: conf.Xcsv2Config.OsmosisRecoveryAddress,
	}

	// Get the Osmosis RPC URI from the mainnet chain registry
	chainClientConfigOsmosis, err := registry.GetChain(context.Background(), "osmosis", logger)
	if err != nil {
		logger.Error("Chain not present in chain registry", zap.String("name", "osmosis"), zap.Error(err))
		return nil, nil, err
	}

	chainClientConfigOsmosis.Modules = cmd.ModuleBasics
	chainClientOsmosis, err := cosmosclient.NewChainClient(logger, chainClientConfigOsmosis, osmosisConfig.HomeDir, nil, nil)
	if err != nil {
		return nil, nil, err
	}

	recoveryAccAddr, err := sdk.AccAddressFromBech32(osmosisConfig.TxSignerAddress)
	if err != nil {
		return nil, nil, err
	}
	kr, err := chainClientOsmosis.Keybase.KeyByAddress(recoveryAccAddr)
	if err != nil {
		return nil, nil, err
	}

	chainClientConfigOsmosis.Key = kr.Name
	osmosisConfig.TxSigner = helpers.CosmosUser{Address: recoveryAccAddr, FromName: chainClientOsmosis.Config.Key}
	osmosisConfig.ChainClient = chainClientOsmosis
	osmosisConfig.ChainClientConfig = chainClientConfigOsmosis

	for chainName, chainConfig := range conf.Chains {
		maxTrade, ok := sdkmath.NewIntFromString(chainConfig.OriginChainTokenInMax)
		if !ok {
			return nil, nil, fmt.Errorf("invalid configuration, %s chain must have parseable Int for param 'token_in_max'", chainName)
		}

		currConf := &Xcsv2OriginChainConfig{
			OriginChainRecipient:       chainConfig.OriginChainRecipient,
			OriginChainName:            chainName,
			StakingAddresses:           strings.Split(chainConfig.OriginChainStakingAddresses, ","),
			logger:                     logger,
			ctx:                        ctx,
			OriginChainHomeDir:         chainConfig.OriginHomeDir,
			OriginChainTxSignerAddress: chainConfig.OriginChainSwapAddress,
			OsmosisRecipientAddress:    chainConfig.OsmosisRecipientAddress,
			OriginChainSwapOutputDenom: chainConfig.OutputDenomOrigin,
			OsmosisOutputDenom:         chainConfig.OutputDenomOsmosis,
			OriginChainTokenInDenom:    chainConfig.OriginChainTokenInDenom,
			OriginChainTokenInMax:      maxTrade,
			OriginToOsmosisSrcChannel:  chainConfig.OriginToOsmosisSrcChannel,
			OriginToOsmosisSrcPort:     chainConfig.OriginToOsmosisSrcPort,
			OriginToOsmosisClientId:    chainConfig.OriginToOsmosisClientId,
		}

		if len(currConf.StakingAddresses) == 0 {
			return nil, nil, fmt.Errorf("invalid configuration, %s chain must have CSV for param 'staking_addresses'", chainName)
		}

		// Get the chain RPC URI from the mainnet chain registry
		chainClientConfig, err := registry.GetChain(context.Background(), chainName, logger)
		if err != nil {
			logger.Error("Chain not present in chain registry", zap.String("name", chainName), zap.Error(err))
			return nil, nil, err
		}

		chainClientConfig.Modules = cmd.ModuleBasics
		currConf.OriginChainClientConfig = chainClientConfig

		chainClient, err := cosmosclient.NewChainClient(logger, chainClientConfig, chainConfig.OriginHomeDir, nil, nil)
		if err != nil {
			return nil, nil, err
		}

		currConf.OriginChainClient = chainClient

		// Ensure the SDK bech32 prefixes are set to "cosmos"
		sdkCtxMu := chainClient.SetSDKContext()

		swapAccAddr, err := sdk.AccAddressFromBech32(chainConfig.OriginChainSwapAddress)
		if err != nil {
			return nil, nil, err
		}
		kr, err := chainClient.Keybase.KeyByAddress(swapAccAddr)
		if err != nil {
			return nil, nil, err
		}

		chainClientConfig.Key = kr.Name
		currConf.OriginChainTxSigner = helpers.CosmosUser{Address: swapAccAddr, FromName: chainClient.Config.Key}

		//txSignerOsmosisAddress := currConf.OriginChainTxSigner.ToBech32("osmo")
		sdkCtxMu()
		swapConfigs = append(swapConfigs, currConf)
	}

	return swapConfigs, osmosisConfig, nil
}

// Gets the XCSv2 routes from the swaprouter contract
func (client *OsmosisClient) GetCrosschainSwapRoutes(swapRouterContractAddress string) {
	wasmQueryClient := wasmtypes.NewQueryClient(client.osmosisClient.CliContext())
	rawStateResp, err := wasmQueryClient.AllContractState(context.Background(), &wasmtypes.QueryAllContractStateRequest{
		Address: swapRouterContractAddress,
	})
	if err != nil {
		return
	}

	//fmt.Printf("%s\n", string(rawStateResp.String()))
	for _, model := range rawStateResp.Models {
		k := hex.EncodeToString(model.Key)
		v := string(model.Value)
		fmt.Printf("Key: %s, Value: %s\n", k, v)
	}
}

// Gets the XCSv2 routes from the swaprouter contract
func (client *OsmosisClient) GetSwapRoute(contractAddress, inputDenom, outputDenom string) (wasm.SwapRoutes, error) {
	query := fmt.Sprintf(`{"get_route": {"input_denom":"%s", "output_denom":"%s"}}`, inputDenom, outputDenom)
	routes := wasm.SwapRoutes{}

	wasmQueryClient := wasmtypes.NewQueryClient(client.osmosisClient.CliContext())
	rawStateResp, err := wasmQueryClient.SmartContractState(context.Background(), &wasmtypes.QuerySmartContractStateRequest{
		Address:   contractAddress,
		QueryData: []byte(query),
	})
	if err != nil {
		return routes, err
	}

	err = json.Unmarshal(rawStateResp.Data, &routes)
	return routes, err
}

func (client *OsmosisClient) GetRawContractState(contractAddress string) (string, error) {
	wasmQueryClient := wasmtypes.NewQueryClient(client.osmosisClient.CliContext())
	resp, err := wasmQueryClient.AllContractState(context.Background(), &wasmtypes.QueryAllContractStateRequest{
		Address: contractAddress,
	})
	if err != nil {
		return "", err
	}

	return resp.String(), nil
}

func (client *OsmosisClient) GetSwapRouterContractAddress() (string, error) {
	wasmQueryClient := wasmtypes.NewQueryClient(client.osmosisClient.CliContext())

	rawStateResp, err := wasmQueryClient.RawContractState(context.Background(), &wasmtypes.QueryRawContractStateRequest{
		Address:   osmosisSwapForwardContract,
		QueryData: []byte("config"),
	})
	if err != nil {
		return "", err
	}

	var dm map[string]string
	err = json.Unmarshal(rawStateResp.Data, &dm)
	if err != nil {
		return "", err
	}
	swapContract, ok := dm["swap_contract"]
	if !ok {
		return "", errors.New("QueryRawContractStateRequest: key swap_contract not present")
	}
	return swapContract, nil
}
