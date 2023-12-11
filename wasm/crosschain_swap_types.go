package wasm

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
