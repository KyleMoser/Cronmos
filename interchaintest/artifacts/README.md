# Building the wasm contracts

Roughly speaking, you can build the contracts by using the instructions at <https://github.com/CosmWasm/rust-optimizer>.

To build, clone the osmosis SDK and cd to the cosmwasm directory, then run e.g.
```docker run --rm -v "$(pwd)":/code   --mount type=volume,source="$(basename "$(pwd)")_cache",target=/target   --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry   cosmwasm/optimizer:0.15.0```.
