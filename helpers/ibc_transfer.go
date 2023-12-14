package helpers

import (
	"context"
	"fmt"

	sdkerrors "cosmossdk.io/errors"
	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/codec"
	transfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	commitmenttypes "github.com/cosmos/ibc-go/v8/modules/core/23-commitment/types"
	host "github.com/cosmos/ibc-go/v8/modules/core/24-host"
	ibcexported "github.com/cosmos/ibc-go/v8/modules/core/exported"
)

const defaultTimeoutOffset = 1000

func PrepareIbcTransfer(chainClient *cosmosclient.ChainClient, height int64, srcChannel, srcPort, clientid string) (*transfertypes.MsgTransfer, error) {
	ctx := context.Background()
	clientState, err := queryClientState(chainClient, ctx, height, clientid)
	if err != nil {
		return nil, err
	}
	clientHeight := clientState.GetLatestHeight().GetRevisionHeight()

	timeoutHeight := clientHeight + defaultTimeoutOffset
	timeoutTimestamp := uint64(0)

	piTimeoutHeight := clienttypes.Height{
		RevisionNumber: clientState.GetLatestHeight().GetRevisionNumber(),
		RevisionHeight: timeoutHeight,
	}

	msg := &transfertypes.MsgTransfer{
		SourceChannel:    srcChannel,
		SourcePort:       srcPort,
		TimeoutTimestamp: timeoutTimestamp,
	}

	if piTimeoutHeight.RevisionHeight != 0 {
		msg.TimeoutHeight = piTimeoutHeight
	}

	return msg, nil
}

// QueryClientState retrieves the latest consensus state for a client in state at a given height
// and unpacks it to exported client state interface
func queryClientState(chainClient *cosmosclient.ChainClient, ctx context.Context, height int64, clientid string) (ibcexported.ClientState, error) {
	clientStateRes, err := queryClientStateResponse(chainClient, ctx, height, clientid)
	if err != nil {
		return nil, err
	}

	clientStateExported, err := clienttypes.UnpackClientState(clientStateRes.ClientState)
	if err != nil {
		return nil, err
	}

	return clientStateExported, nil
}

func queryClientStateResponse(chainClient *cosmosclient.ChainClient, ctx context.Context, height int64, srcClientId string) (*clienttypes.QueryClientStateResponse, error) {
	key := host.FullClientStateKey(srcClientId)

	value, proofBz, proofHeight, err := queryTendermintProof(chainClient, ctx, height, key)
	if err != nil {
		return nil, err
	}

	// check if client exists
	if len(value) == 0 {
		return nil, sdkerrors.Wrap(clienttypes.ErrClientNotFound, srcClientId)
	}

	cdc := codec.NewProtoCodec(chainClient.Codec.InterfaceRegistry)

	clientState, err := clienttypes.UnmarshalClientState(cdc, value)
	if err != nil {
		return nil, err
	}

	anyClientState, err := clienttypes.PackClientState(clientState)
	if err != nil {
		return nil, err
	}

	return &clienttypes.QueryClientStateResponse{
		ClientState: anyClientState,
		Proof:       proofBz,
		ProofHeight: proofHeight,
	}, nil
}

// QueryTendermintProof performs an ABCI query with the given key and returns
// the value of the query, the proto encoded merkle proof, and the height of
// the Tendermint block containing the state root. The desired tendermint height
// to perform the query should be set in the client context. The query will be
// performed at one below this height (at the IAVL version) in order to obtain
// the correct merkle proof. Proof queries at height less than or equal to 2 are
// not supported. Queries with a client context height of 0 will perform a query
// at the latest state available.
// Issue: https://github.com/cosmos/cosmos-sdk/issues/6567
func queryTendermintProof(chainClient *cosmosclient.ChainClient, ctx context.Context, height int64, key []byte) ([]byte, []byte, clienttypes.Height, error) {
	// ABCI queries at heights 1, 2 or less than or equal to 0 are not supported.
	// Base app does not support queries for height less than or equal to 1.
	// Therefore, a query at height 2 would be equivalent to a query at height 3.
	// A height of 0 will query with the lastest state.
	if height != 0 && height <= 2 {
		return nil, nil, clienttypes.Height{}, fmt.Errorf("proof queries at height <= 2 are not supported")
	}

	// Use the IAVL height if a valid tendermint height is passed in.
	// A height of 0 will query with the latest state.
	if height != 0 {
		height--
	}

	req := abci.RequestQuery{
		Path:   fmt.Sprintf("store/%s/key", ibcexported.StoreKey),
		Height: height,
		Data:   key,
		Prove:  true,
	}

	res, err := chainClient.QueryABCI(ctx, req)
	if err != nil {
		return nil, nil, clienttypes.Height{}, err
	}

	merkleProof, err := commitmenttypes.ConvertProofs(res.ProofOps)
	if err != nil {
		return nil, nil, clienttypes.Height{}, err
	}

	cdc := codec.NewProtoCodec(chainClient.Codec.InterfaceRegistry)

	proofBz, err := cdc.Marshal(&merkleProof)
	if err != nil {
		return nil, nil, clienttypes.Height{}, err
	}

	revision := clienttypes.ParseChainID(chainClient.Config.ChainID)
	return res.Value, proofBz, clienttypes.NewHeight(revision, uint64(res.Height)+1), nil
}
