package helpers

import (
	"os"

	"gopkg.in/yaml.v3"
)

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
