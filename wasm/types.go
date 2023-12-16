package wasm

// The main msg types for Osmosis XCS v2 TXs
type ExecuteMsg struct {
	Xcsv2Swap               *CrosschainSwap          `json:"osmosis_swap,omitempty"`
	Swap                    *SwapRouterSwap          `json:"swap,omitempty"`
	SetRoute                *SwapRouterSetRoute      `json:"set_route,omitempty"`
	ModifyBech32Prefixes    *ModifyBech32Prefixes    `json:"modify_bech32_prefixes,omitempty"`
	ModifyChainChannelLinks *ModifyChainChannelLinks `json:"modify_chain_channel_links,omitempty"`
}
