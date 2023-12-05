package interchaintest_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/KyleMoser/Cronmos/wasm"
)

func TestPrintRoutes(t *testing.T) {
	// List of routes that might not exist on Osmosis mainnet crosschain swap router contract:
	// Agoric, Akash, Celestia, Cosmoshub, Crescent, DYDX, Evmos, Injective, Juno, Neutron, Nibiru, Noble, Nolus, Omniflixhub, Osmosis, Quasar
	// Stargaze, Stride, Sentinelhub
	denomOsmo := "uosmo"
	denomNobleUsdc := "ibc/498A0751C798A0D9A389AA3691123DADA57DAA4FE165D5C75894505B876BA6E4"
	denomAkash := "ibc/1480B8FD20AD5FCAE81EA87584D269547DD4D436843C1D20F15E00EB64743EF4"  //AKT
	denomAgoric := "ibc/2DA9C149E9AD2BD27FEFA635458FB37093C256C1A940392634A16BEA45262604" // UBLD

	routeBldOsmo := wasm.PoolRoute{
		PoolID:        "795",
		TokenOutDenom: denomOsmo,
	}
	routeOsmoUsdc := wasm.PoolRoute{
		PoolID:        "1221",
		TokenOutDenom: denomNobleUsdc,
	}
	routeAktOsmo := wasm.PoolRoute{
		PoolID:        "1093",
		TokenOutDenom: denomNobleUsdc,
	}

	// BLD/OSMO -> OSMO/USDC
	msgSetRoute1 := wasm.ExecuteMsg{
		SetRoute: &wasm.SwapRouterSetRoute{
			InputDenom:  denomAgoric,
			OutputDenom: denomOsmo,
			PoolRoutes:  []wasm.PoolRoute{routeBldOsmo, routeOsmoUsdc},
		},
	}

	// AKT/OSMO -> OSMO/USDC
	msgSetRoute2 := wasm.ExecuteMsg{
		SetRoute: &wasm.SwapRouterSetRoute{
			InputDenom:  denomAkash,
			OutputDenom: denomOsmo,
			PoolRoutes:  []wasm.PoolRoute{routeAktOsmo, routeOsmoUsdc},
		},
	}

	b, _ := json.Marshal(msgSetRoute1)
	fmt.Println(string(b))
	b, _ = json.Marshal(msgSetRoute2)
	fmt.Println(string(b))
}
