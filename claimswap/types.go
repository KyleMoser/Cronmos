package claimswap

import (
	"context"
	"os"
	"path/filepath"

	sdkmath "cosmossdk.io/math"
	"github.com/KyleMoser/Cronmos/helpers"
	cosmosclient "github.com/KyleMoser/cosmos-client/client"
	"go.uber.org/zap"
)

var (
	osmosisSwapForwardContract = "osmo1uwk8xc6q0s6t5qcpr6rht3sczu6du83xq8pwxjua0hfj5hzcnh3sqxwvxs"
	chainRegOsmosis            = "osmosis"

	// Mapping of the chain native token to the Osmosis IBC prefixed denom.
	// Reference: https://raw.githubusercontent.com/osmosis-labs/assetlists/main/osmosis-1/osmosis-1.assetlist.json.
	TokenDenomMapping = map[string]string{
		"OSMO":    "uosmo",                                                                // Osmosis
		"UBLD":    "ibc/2DA9C149E9AD2BD27FEFA635458FB37093C256C1A940392634A16BEA45262604", // Agoric
		"USDC":    "ibc/498A0751C798A0D9A389AA3691123DADA57DAA4FE165D5C75894505B876BA6E4", // Noble USDC
		"AKT":     "ibc/1480B8FD20AD5FCAE81EA87584D269547DD4D436843C1D20F15E00EB64743EF4", // Akash
		"TIA":     "ibc/D79E7D83AB399BFFF93433E54FAA480C191248FC556924A2A8351AE2638B3877", // Celestia
		"ATOM":    "ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2", // Cosmos Hub
		"CRE":     "ibc/5A7C219BA5F7582B99629BA3B2A01A61BFDA0F6FD1FE95B5366F7334C4BC0580", // Crescent
		"DYDX":    "ibc/831F0B1BBB1D08A2B75311892876D71565478C532967545476DF4C2D7492E48C", // DYDX
		"EVMOS":   "ibc/6AE98883D4D5D5FF9E50D7130F1305DA2FFA0C652D1DD9C123657C6B4EB2DF8A", // Evmos
		"INJ":     "ibc/64BA6E31FE887D66C6F8F31C7B1A80C7CA179239677B4088BB55F5EA07DBE273", // Injective
		"JUNO":    "ibc/46B44899322F3CD854D2D46DEEF881958467CDD4B3B10086DA49296BBED94BED", // ujuno on Juno
		"NTRN":    "ibc/126DA09104B71B164883842B769C0E9EC1486C0887D27A9999E395C2C8FB5682", // Neutron
		"NLS":     "ibc/D9AFCECDD361D38302AA66EB3BAC23B95234832C51D12489DC451FA2B7C72782", // Nolus
		"FLIX":    "ibc/CEE970BB3D26F4B907097B6B660489F13F3B0DA765B83CC7D9A0BC0CE220FA6F", // Omniflix
		"QSR":     "ibc/1B708808D372E959CD4839C594960309283424C775F4A038AAEBE7F83A988477", // Quasar
		"STARS":   "ibc/987C17B11ABC2B20019178ACE62929FE9840202CE79498E29FE8E5CB02B7C0A4", // Stargaze
		"STRD":    "ibc/A8CA5EE328FA10C9519DF6057DA1F69682D28F7D0F5CCC7ECB72E3DCA2D157A4", // Stride
		"DVPN":    "ibc/9712DBB13B9631EDFA9BF61B55F1B2D290B2ADB67E3A4EB3A875F3B6081B3B84", // DVPN
		"AXLUSDC": "ibc/D189335C6E4A68B513C10AB227BF1C1D38C746766278BA3EEB4FB14124F1D858", // Axelar USDC
	}
)

// Given the token symbol (e.g. ATOM) from the Osmosis assetlist, return the denom
func GetOsmosisDenomForToken(tokenSymbol string) string {
	return TokenDenomMapping[tokenSymbol]
}

// Client primarily for crosschain swap (xcsv2) queries and TXs
type OsmosisClient struct {
	osmosisClient *cosmosclient.ChainClient
}

type Xcsv2OriginChainConfig struct {
	logger                      *zap.Logger
	ctx                         context.Context
	ValidatorAddress            string
	DelegatorAddresses          []string
	OriginChainHomeDir          string
	OriginChainTxSignerAddress  string
	OriginChainTokenInDenom     string
	OriginChainTokenInMax       sdkmath.Int
	OsmosisXcsv2RecoveryAddress string // The XCSv2 recovery address
	OriginChainSwapOutputDenom  string // The output denom of the XCSv2 trade, as represented on the origin chain (not on Osmosis)
	OsmosisOutputDenom          string // The output denom of the trade on osmosis. For e.g. USDC, this will start with ibc/
	OriginChainClientConfig     *cosmosclient.ChainClientConfig
	OriginChainClient           *cosmosclient.ChainClient
	OriginChainTxSigner         helpers.CosmosUser
	OriginChainName             string
	OriginToOsmosisSrcChannel   string
	OriginToOsmosisSrcPort      string
	OriginToOsmosisClientId     string
}

type Xcsv2OsmosisConfig struct {
	HomeDir            string
	TxSignerAddress    string
	ChainClientConfig  *cosmosclient.ChainClientConfig
	ChainClient        *cosmosclient.ChainClient
	TxSigner           helpers.CosmosUser
	DestinationAddress string
}

func NewClient(osmosisClient *cosmosclient.ChainClient) *OsmosisClient {
	return &OsmosisClient{
		osmosisClient: osmosisClient,
	}
}

// Searches the Osmosis chain registry for an available, healthy RPC endpoint
// Uses OSMOSIS_HOME as your Osmosis home directory, or defaults to $HOME/.osmosisd
func NewClientFromRegistry(logger *zap.Logger) (*OsmosisClient, error) {
	// local home directory for Osmosis
	osmosisHome, ok := os.LookupEnv("OSMOSIS_HOME")
	if !ok {
		osmosisHome, err := os.UserHomeDir()
		if err != nil {
			panic("Set OSMOSIS_HOME env to Osmosis home directory")
		}
		osmosisHome = filepath.Join(osmosisHome, ".osmosisd")
		logger.Warn("OSMOSIS_HOME env not set", zap.String("Using default home directory", osmosisHome))
	}

	// Get the chain RPC URI from the Osmosis mainnet chain registry
	ccc, err := cosmosclient.GetChain(context.Background(), chainRegOsmosis, logger)
	if err != nil {
		return nil, err
	}
	cc, err := cosmosclient.NewChainClient(logger, ccc, osmosisHome, nil, nil)
	if err != nil {
		return nil, err
	}

	return &OsmosisClient{
		osmosisClient: cc,
	}, nil
}
