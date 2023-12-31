package helpers

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type CosmosUser struct {
	Address  sdk.AccAddress
	FromName string
}

func (user *CosmosUser) KeyName() string {
	return user.FromName
}
func (user *CosmosUser) ToBech32(prefix string) string {
	return sdk.MustBech32ifyAddressBytes(prefix, user.Address)
}
func (user *CosmosUser) FormattedAddress() string {
	return user.Address.String()
}
