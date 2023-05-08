package keepers

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type (
	TickerPrice struct {
		Price  sdk.Dec // last trade price
		Volume sdk.Dec // 24h volume
	}

	CandlePrice struct {
		Price     sdk.Dec // last trade price
		Volume    sdk.Dec // volume
		TimeStamp int64   // timestamp
	}

	// AggregatedProviderPrices defines a type alias for a map
	// of provider -> asset -> TickerPrice
	AggregatedProviderPrices map[string]map[string]TickerPrice

	// AggregatedProviderCandles defines a type alias for a map
	// of provider -> asset -> []types.CandlePrice
	AggregatedProviderCandles map[Name]map[string][]types.CandlePrice
)

type CurrencyPair struct {
	Base  string
	Quote string
}

func (cp CurrencyPair) String() string {
	return cp.Base + cp.Quote
}

type FauxOracleKeeper struct{}

func (k FauxOracleKeeper) GetSupportedPairs() []CurrencyPair {
	return []CurrencyPair{
		{Base: "ATOM", Quote: "USD"},
	}
}
