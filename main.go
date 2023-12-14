package main

import (
	"flag"
	"log"

	"github.com/KyleMoser/Cronmos/helpers"
	"github.com/KyleMoser/Cronmos/osmosis"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var config string

func main() {
	sdk.SetAddrCacheEnabled(false)

	flag.StringVar(&config, "config", "", "path to config for the application")
	flag.Parse()

	conf, err := helpers.ReadYamlConfig(config)
	if err != nil {
		log.Fatal(err.Error())
	}

	xcsv2Config, osmoConfig, err := osmosis.ToXcsv2Config(conf)
	if err != nil {
		log.Fatal(err.Error())
	}

	for _, chain := range xcsv2Config {
		err := osmosis.CrosschainSwap(chain, osmoConfig)
		if err != nil {
			log.Fatal(err.Error())
		}
	}
}
