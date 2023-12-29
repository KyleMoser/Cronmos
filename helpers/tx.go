package helpers

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"
)

func GetTx(queryContext client.Context, txHash string) (*txTypes.Tx, error) {
	req := &txTypes.GetTxRequest{Hash: txHash}
	txQueryClient := txTypes.NewServiceClient(queryContext)
	txRes, err := txQueryClient.GetTx(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return txRes.Tx, nil
}
