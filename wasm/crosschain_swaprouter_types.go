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
