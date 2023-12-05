package wasm

// The types you can use for CosmWasm TXs are all in here
type ExecuteMsg struct {
	Swap     *CrosschainSwap     `json:"osmosis_swap,omitempty"`
	SetRoute *SwapRouterSetRoute `json:"set_route,omitempty"`
}

// Msg type to set a swap route for crosschain swap router contract
type SwapRouterSetRoute struct {
	InputDenom  string      `json:"input_denom"`
	OutputDenom string      `json:"output_denom"`
	PoolRoutes  []PoolRoute `json:"pool_route"`
}

type PoolRoute struct {
	PoolID        string `json:"pool_id"`
	TokenOutDenom string `json:"token_out_denom"`
}

// Msg type to perform a swap using Osmosis crosschain swap contract
type CrosschainSwap struct {
	NextMemo         *string `json:"next_memo"`
	OnFailedDelivery `json:"on_failed_delivery"`
	OutputDenom      string `json:"output_denom"`
	Receiver         string `json:"receiver"`
	Slippage         `json:"slippage"`
}

type OnFailedDelivery struct {
	RecoveryAddress string `json:"local_recovery_addr"`
}

type Slippage struct {
	Twap `json:"twap"`
}

type Twap struct {
	Percentage string `json:"slippage_percentage"`
	Window     int    `json:"window_seconds"`
}
