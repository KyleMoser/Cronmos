package interchaintest_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/KyleMoser/Cronmos/claimswap"
	"github.com/KyleMoser/Cronmos/logging"
	"github.com/KyleMoser/Cronmos/wasm"
	"github.com/stretchr/testify/require"
)

func TestGetMainnetXcsv2Config(t *testing.T) {
	logger := logging.DoConfigureLogger("DEBUG")
	osmoClient, err := claimswap.NewClientFromRegistry(logger)
	require.NoError(t, err)

	osmoClient.PrintContractStateModels(osmosisSwapForwardContract)

	swapRouterContract, err := osmoClient.GetSwapRouterContractAddress()
	require.NoError(t, err)
	osmoClient.PrintContractStateModels(swapRouterContract)
}

func TestGetSwapRoutes(t *testing.T) {
	logger := logging.DoConfigureLogger("DEBUG")
	osmoClient, err := claimswap.NewClientFromRegistry(logger)
	require.NoError(t, err)
	swapRouterContract, err := osmoClient.GetSwapRouterContractAddress()
	require.NoError(t, err)

	// There are no routes on mainnet for this token.
	// Using AXLUSDC or OSMO, you can see that this code works.
	outputToken := "USDC"

	for inputTokenSymbol, inputToken := range claimswap.TokenDenomMapping {
		// Find the existing route to trade the given input token to 'outputToken'
		if inputTokenSymbol != outputToken {
			route, err := osmoClient.GetSwapRoute(swapRouterContract, inputToken, claimswap.GetOsmosisDenomForToken(outputToken))
			if err != nil {
				fmt.Printf("No trade route found. Input token symbol: %s, output token: %s\n", inputTokenSymbol, outputToken)
			} else {
				fmt.Printf("Trade route found for (%s->%s): %+v\n", inputTokenSymbol, outputToken, route)
			}
		}
	}
}

func TestPrintRoutes(t *testing.T) {
	// List of routes that might not exist on Osmosis mainnet crosschain swap router contract:
	// Agoric, Akash, Celestia, Cosmoshub, Crescent, DYDX, Evmos, Injective, Juno, Neutron, Noble, Nolus, Omniflixhub, Osmosis, Quasar
	// Stargaze, Stride, Sentinelhub
	// Nibiru ?? -- cannot find this token in the registry

	routeBldOsmo := wasm.PoolRoute{
		PoolID:        "795",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("OSMO"),
	}
	routeCreOsmo := wasm.PoolRoute{
		PoolID:        "786",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("OSMO"),
	}
	routeOsmoUsdc := wasm.PoolRoute{
		PoolID:        "1221",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("USDC"),
	}
	routeAktOsmo := wasm.PoolRoute{
		PoolID:        "1093",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("OSMO"),
	}
	routeDydxUsdc := wasm.PoolRoute{
		PoolID:        "1246",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("USDC"),
	}
	routeNlsAxlUsdc := wasm.PoolRoute{
		PoolID:        "1041",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("AXLUSDC"),
	}
	routeAxlUsdcNobleUsdc := wasm.PoolRoute{
		PoolID:        "1212",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("USDC"),
	}
	routeQsrOsmo := wasm.PoolRoute{
		PoolID:        "1060",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("OSMO"),
	}
	routeAtomUsdc := wasm.PoolRoute{
		PoolID:        "1251",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("USDC"),
	}
	routeNtrnUsdc := wasm.PoolRoute{
		PoolID:        "1324",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("USDC"),
	}
	routeFlixOsmo := wasm.PoolRoute{
		PoolID:        "992",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("OSMO"),
	}
	routeEvmosOsmo := wasm.PoolRoute{
		PoolID:        "722",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("OSMO"),
	}
	routeInjUsdc := wasm.PoolRoute{
		PoolID:        "1319",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("USDC"),
	}
	routeJunoOsmo := wasm.PoolRoute{
		PoolID:        "1097",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("OSMO"),
	}
	routeStrdOsmo := wasm.PoolRoute{
		PoolID:        "1098",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("OSMO"),
	}
	routeDvpnOsmo := wasm.PoolRoute{
		PoolID:        "5", // 1108 would also work, but has significantly lower liquidity.
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("OSMO"),
	}
	routeTiaUsdc := wasm.PoolRoute{
		PoolID:        "1247",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("USDC"),
	}
	routeStarsOsmo := wasm.PoolRoute{
		PoolID:        "1096",
		TokenOutDenom: claimswap.GetOsmosisDenomForToken("OSMO"),
	}

	setRouteMsgs := []wasm.ExecuteMsg{
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("UBLD"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeBldOsmo, routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("AKT"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeAktOsmo, routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("OSMO"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("OSMO"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("CRE"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeCreOsmo, routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("DYDX"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeDydxUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("NLS"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeNlsAxlUsdc, routeAxlUsdcNobleUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("QSR"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeQsrOsmo, routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("ATOM"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeAtomUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("NTRN"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeNtrnUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("FLIX"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeFlixOsmo, routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("EVMOS"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeEvmosOsmo, routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("INJ"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeInjUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("JUNO"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeJunoOsmo, routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("STRD"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeStrdOsmo, routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("DVPN"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeDvpnOsmo, routeOsmoUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("AXLUSDC"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeAxlUsdcNobleUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("TIA"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeTiaUsdc},
			},
		},
		{
			SetRoute: &wasm.SwapRouterSetRoute{
				InputDenom:  claimswap.GetOsmosisDenomForToken("STARS"),
				OutputDenom: claimswap.GetOsmosisDenomForToken("USDC"),
				PoolRoutes:  []wasm.PoolRoute{routeStarsOsmo, routeOsmoUsdc},
			},
		},
	}

	for i, route := range setRouteMsgs {
		res, _ := json.Marshal(route.SetRoute.PoolRoutes)
		fmt.Printf("(%d) Input token: %s, output token: %s, route: %s\n\n", i+1, route.SetRoute.InputDenom, route.SetRoute.OutputDenom, string(res))
	}
}
