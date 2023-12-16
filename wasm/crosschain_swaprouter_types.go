package wasm

// Msg type to set a swap route for crosschain swap router contract
type SwapRouterSetRoute struct {
	InputDenom  string      `json:"input_denom"`
	OutputDenom string      `json:"output_denom"`
	PoolRoutes  []PoolRoute `json:"pool_route"`
}

type SwapRoutes struct {
	PoolRoutes []PoolRoute `json:"pool_route"`
}

type PoolRoute struct {
	PoolID        string `json:"pool_id"`
	TokenOutDenom string `json:"token_out_denom"`
}

// The swaprouter has a `Swap` message type. When conducting crosschain swaps (IBC transfer to Osmosis with a memo),
// there are two choices for the contract you should invoke. If the destination (of the output token after the swap)
// is Osmosis, then you should call the swaprouter contract's `Swap` message. Conversely, if the destination
// is any other chain, the funds will need to be IBC transferred (off of Osmosis), and you should use the XCSv2 contract.
type SwapRouterSwap struct {
	InputCoin   Coin                `json:"input_coin"`
	OutputDenom string              `json:"output_denom"`
	Slippage    SwapRouterSlippage  `json:"slippage"`
	Route       []SwapAmountInRoute `json:"route,omitempty"`
}

type SwapRouterSlippage struct {
	SwapRouterTwap `json:"twap"`
	//MinOutputAmount uint64 `json:"min_output_amount"`
}

type SwapRouterTwap struct {
	Percentage string `json:"slippage_percentage"`
	Window     uint64 `json:"window_seconds"`
}

type Coin struct {
	Denom  string `json:"denom"`
	Amount string `json:"amount"`
}

type SwapAmountInRoute struct {
	PoolID        uint64 `json:"pool_id"`
	TokenOutDenom string `json:"token_out_denom"`
}
