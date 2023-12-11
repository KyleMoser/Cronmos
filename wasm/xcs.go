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

func CrosschainSwapMemo(recoveryAddress, fundRecipient, outputDenom, xcsContractAddress string) (string, error) {
	msg := MemoWrapperOuter{
		XcsMemo: MemoWrapperInner{
			Contract: xcsContractAddress,
			Msg: ExecuteMsg{
				Swap: &CrosschainSwap{
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
