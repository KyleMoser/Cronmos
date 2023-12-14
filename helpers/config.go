package helpers

import (
	"os"

	"gopkg.in/yaml.v3"
)

// chains:
//   cosmoshub: # Origin chain that matches the directory name in the chain registry here: https://github.com/cosmos/chain-registry.
//     stakingAddresses: cosmos...,cosmos2... # Must grant the swap address ability to claim validator rewards and IBC transfer
//     swapAddress: cosmos... # Must be granted ability to claim validator rewards and IBC transfer
//     home: /home/kyle/.gaia # local home directory for the chain
//     output_denom_osmosis: uosmo # the output denom of the trade, as represented on osmosis. For e.g. USDC, this will start with ibc/
//     output_denom_origin: ibc/something # the output denom of the trade, as represented on the origin chain

type Config struct {
	Chains      map[string]ChainXcsv2Config `yaml:"chains"`
	Xcsv2Config OsmosisXcsv2Config          `yaml:"xcsv2"`
}

type OsmosisXcsv2Config struct {
	OsmosisRecoveryAddress string `yaml:"osmosis_recipient"`
	OsmosisHome            string `yaml:"home"`
}

type ChainXcsv2Config struct {
	OriginChainStakingAddresses string `yaml:"staking_addresses"`
	OriginChainSwapAddress      string `yaml:"swap_address"`
	OriginHomeDir               string `yaml:"home"`
	OutputDenomOsmosis          string `yaml:"output_denom_osmosis"`
	OutputDenomOrigin           string `yaml:"output_denom_origin"`
	OsmosisRecipientAddress     string `yaml:"osmosis_recipient"`
	OriginChainTokenInDenom     string `yaml:"token_in_denom"`
	OriginChainTokenInMax       string `yaml:"token_in_max"`
	OriginChainRecipient        string `yaml:"origin_chain_recipient"`
	OriginToOsmosisSrcChannel   string `yaml:"src_channel"`
	OriginToOsmosisSrcPort      string `yaml:"src_port"`
	OriginToOsmosisClientId     string `yaml:"client_id"`
}

func ReadYamlConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var conf Config
	err = yaml.Unmarshal(b, &conf)
	if err != nil {
		return nil, err
	}

	return &conf, nil
}
