package osmosis

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/KyleMoser/Cronmos/wasm"
)

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
