package wasm

// Unimplemented: not needed
type ModifyContractAlias struct {
}

// Allows the owner (or an authorized address for a specific source_chain) to create, update, or delete
// IBC channel links between each chain. The operation expects a vector of ConnectionOperation,
// where each operation is either a CreateConnection, UpdateConnection, or DeleteConnection operation.
type ModifyChainChannelLinks struct {
	Operations []ConnectionInput `json:"operations"`
}

// The ModifyBech32Prefixes operation allows the owner (or an authorized address for a specific source_chain)
// to create, update, or delete Bech32 prefixes for each chain. The operation expects a vector of ChainToPrefixOperation,
// where each operation is either a CreatePrefix, UpdatePrefix, or DeletePrefix operation.
type ModifyBech32Prefixes struct {
	Operations []ChainToBech32PrefixInput `json:"operations"`
}

type ChainToBech32PrefixInput struct {
	Operation string `json:"operation"` // set, change, remove, enable, disable
	ChainName string `json:"chain_name"`
	Prefix    string `json:"prefix"`
}

type ConnectionInput struct {
	Operation   string `json:"operation"` // set, change, remove, enable, disable
	SourceChain string `json:"source_chain"`
	DestChain   string `json:"destination_chain"`
	ChannelID   string `json:"channel_id"`
}
