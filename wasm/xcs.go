package wasm

import (
	"encoding/json"
)

type MemoWrapperOuter struct {
	XcsMemo MemoWrapperInner `json:"wasm"`
}
type MemoWrapperInner struct {
	Contract string     `json:"contract"`
	Msg      ExecuteMsg `json:"msg"` // Only set ExecuteMsg.Swap
}

func SwapRouterMemo(inputCoin Coin, outputDenom, swapRouterContract string) (string, error) {
	msg := MemoWrapperOuter{
		XcsMemo: MemoWrapperInner{
			Contract: swapRouterContract,
			Msg: ExecuteMsg{
				Swap: &SwapRouterSwap{
					InputCoin: inputCoin,
					Slippage: SwapRouterSlippage{
						SwapRouterTwap: SwapRouterTwap{
							Percentage: "5",
							Window:     10,
						},
					},
					OutputDenom: outputDenom,
				},
			},
		},
	}

	b, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func CrosschainSwapMemo(recoveryAddress, fundRecipient, outputDenom, xcsContractAddress string) (string, error) {
	msg := MemoWrapperOuter{
		XcsMemo: MemoWrapperInner{
			Contract: xcsContractAddress,
			Msg: ExecuteMsg{
				Xcsv2Swap: &CrosschainSwap{
					OutputDenom: outputDenom,
					Receiver:    fundRecipient, // Who will get the funds on the other chain
					Slippage: Slippage{
						Twap: Twap{
							Percentage: "5",
							Window:     10,
						},
					},
					OnFailedDelivery: OnFailedDelivery{
						RecoveryAddress: recoveryAddress,
					},
				},
			},
		},
	}

	b, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
