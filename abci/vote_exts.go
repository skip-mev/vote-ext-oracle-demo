package abci

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cometbft/cometbft/libs/log"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/neilotoole/errgroup"
	"github.com/skip-mev/vote-ext-oracle-demo/keepers"
)

// Provider defines an interface for fetching prices and candles for a given set
// of currency pairs. The provider is presumed to be a trusted source of prices.
type Provider interface {
	GetTickerPrices(...keepers.CurrencyPair) (map[string]keepers.TickerPrice, error)
	GetCandlePrices(...keepers.CurrencyPair) (map[string][]keepers.CandlePrice, error)
}

// ProviderAggregator is a simple aggregator for provider prices and candles.
// It is thread-safe since it is assumed to be called concurrently in price
// fetching goroutines, i.e. ExtendVote.
type ProviderAggregator struct {
	mtx sync.Mutex

	providerPrices  keepers.AggregatedProviderPrices
	providerCandles keepers.AggregatedProviderCandles
}

func NewProviderAggregator() *ProviderAggregator {
	return &ProviderAggregator{
		providerPrices:  make(keepers.AggregatedProviderPrices),
		providerCandles: make(keepers.AggregatedProviderCandles),
	}
}

func (p *ProviderAggregator) SetProviderTickerPricesAndCandles(
	providerName string,
	prices map[string]keepers.TickerPrice,
	candles map[string][]keepers.CandlePrice,
	pair keepers.CurrencyPair,
) bool {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	// set prices and candles for this provider if we haven't seen it before
	if _, ok := p.providerPrices[providerName]; !ok {
		p.providerPrices[providerName] = make(map[string]keepers.TickerPrice)
	}
	if _, ok := p.providerCandles[providerName]; !ok {
		p.providerCandles[providerName] = make(map[string][]keepers.CandlePrice)
	}

	// set price for provider/base (e.g. Binance -> ATOM -> 11.98)
	tp, pricesOk := prices[pair.String()]
	if pricesOk {
		p.providerPrices[providerName][pair.Base] = tp
	}

	// set candle for provider/base (e.g. Binance -> ATOM-> [<11.98, 24000, 12:00UTC>])
	cp, candlesOk := candles[pair.String()]
	if candlesOk {
		p.providerCandles[providerName][pair.Base] = cp
	}

	// return true if we set at least one price or candle
	return pricesOk || candlesOk
}

// OracleVoteExtension defines the canonical vote extension structure.
type OracleVoteExtension struct {
	Height int64
	Prices map[string]sdk.Dec
}

// VoteExtHandler defines a handler which implements the ExtendVote and
// VerifyVoteExtension ABCI methods. This handler is to be instantiated and set
// on the BaseApp when constructing an application.
//
// For demo purposes, we presume the the application, via some module, maintains
// a list of asset pairs (base/quote) that it will produce and accept votes for.
//
// For each asset pair, to produce a vote extension, the handler will fetch the
// latest prices from a trusted set of oracle sources, serialize this in a
// structure the application can unmarshal and understand.
//
// For each incoming vote extension that CometBFT asks us to verify, we will ensure
// it is within some safe range of the latest price we have seen for those assets.
type VoteExtHandler struct {
	logger          log.Logger
	currentBlock    int64                             // current block height
	lastPriceSyncTS time.Time                         // last time we synced prices
	providerTimeout time.Duration                     // timeout for fetching prices from providers
	providers       map[string]Provider               // mapping of provider name to provider (e.g. Binance -> BinanceProvider)
	providerPairs   map[string][]keepers.CurrencyPair // mapping of provider name to supported pairs (e.g. Binance -> [ATOM/USD])
	computedPrices  map[int64]map[string]sdk.Dec      // mapping of block height to computed oracle prices (used for verification)

	FauxOracleKeeper keepers.FauxOracleKeeper
}

func (h *VoteExtHandler) ExtendVoteHandler() ExtendVoteHandler {
	return func(ctx sdk.Context, req *RequestExtendVote) (*ResponseExtendVote, error) {
		h.currentBlock = req.Height
		h.lastPriceSyncTS = time.Now()

		h.logger.Info("computing oracle prices for vote extension", "height", req.Height, "time", h.lastPriceSyncTS)

		g := new(errgroup.Group)
		providerAgg := NewProviderAggregator()
		requiredRates := make(map[string]struct{})

		for providerName, currencyPairs := range h.providerPairs {
			priceProvider := h.providers[providerName]

			// create/update set of all required prices for base assets
			for _, pair := range currencyPairs {
				if _, ok := requiredRates[pair.Base]; !ok {
					requiredRates[pair.Base] = struct{}{}
				}
			}

			// Launch a goroutine to fetch ticker prices from this oracle provider.
			// Recall, vote extensions are not required to be deterministic.
			g.Go(func() error {
				doneCh := make(chan bool, 1)
				errCh := make(chan error, 1)

				var (
					prices  map[string]keepers.TickerPrice
					candles map[string][]keepers.CandlePrice
					err     error
				)

				go func() {
					prices, err = priceProvider.GetTickerPrices(currencyPairs...)
					if err != nil {
						h.logger.Error("failed to fetch ticker prices from provider", "provider", providerName, "err", err)
						errCh <- err
					}

					candles, err = priceProvider.GetCandlePrices(currencyPairs...)
					if err != nil {
						h.logger.Error("failed to fetch candle prices from provider", "provider", providerName, "err", err)
						errCh <- err
					}

					doneCh <- true
				}()

				select {
				case <-doneCh:
					break

				case err := <-errCh:
					return err

				case <-time.After(h.providerTimeout):
					return fmt.Errorf("provider %s timed out", providerName)
				}

				// aggregate and collect prices based on the base currency per provider
				for _, pair := range currencyPairs {
					success := providerAgg.SetProviderTickerPricesAndCandles(providerName, prices, candles, pair)
					if !success {
						return fmt.Errorf("failed to find any exchange rates in provider responses")
					}
				}

				return nil
			})
		}

		if err := g.Wait(); err != nil {
			// We failed to get some or all prices from providers. In the case that
			// all prices fail, computeOraclePrices below will return an error.
			h.logger.Error("failed to get ticker prices", "err", err)
		}

		computedPrices, err := h.computeOraclePrices(providerAgg)
		if err != nil {
			// NOTE: The Cosmos SDK will ensure any error returned is captured and
			// logged. We can return nil here to indicate we do not want to produce
			// a vote extension, and thus an empty vote extension will be provided
			// automatically to CometBFT.
			return nil, err
		}

		for base := range requiredRates {
			if _, ok := computedPrices[base]; !ok {
				// In the case where we fail to retrieve latest prices for a supported
				// pair, applications may have different strategies. For example, they
				// may ignore this situation entirely and rely on stale prices, or they
				// may choose to not produce a vote and instead error. We perform the
				// latter here.
				return nil, fmt.Errorf("failed to find price for %s", base)
			}
		}

		// produce a canonical vote extension
		voteExt := OracleVoteExtension{
			Height: req.Height,
			Prices: computedPrices,
		}

		// NOTE: We use stdlib JSON encoding, but an application may choose to use
		// a performant mechanism. This is for demo purposes only.
		bz, err := json.Marshal(voteExt)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal vote extension: %w", err)
		}

		// TODO/XXX: A real application would likely want to persist these prices
		// and ensure they're pruned when no longer needed.
		h.computedPrices[req.Height] = computedPrices

		return &ResponseExtendVote{VoteExtension: bz}, nil
	}
}

func (h *VoteExtHandler) VerifyVoteExtensionHandler() VerifyVoteExtensionHandler {
	return func(ctx sdk.Context, req *RequestVerifyVoteExtension) (*ResponseVerifyVoteExtension, error) {
		var voteExt OracleVoteExtension

		err := json.Unmarshal(req.VoteExtension, &voteExt)
		if err != nil {
			// NOTE: It is safe to return an error as the Cosmos SDK will capture all
			// errors, log them, and reject the proposal.
			return nil, fmt.Errorf("failed to unmarshal vote extension: %w", err)
		}

		if voteExt.Height != req.Height {
			return nil, fmt.Errorf("vote extension height does not match request height; expected: %d, got: %d", req.Height, voteExt.Height)
		}

		if err := h.verifyPrices(ctx, voteExt.Prices); err != nil {
			return nil, fmt.Errorf("failed to verify oracle prices from validator %X: %w", req.ValidatorAddress, err)
		}

		return &ResponseVerifyVoteExtension{Status: ResponseVerifyVoteExtension_ACCEPT}, nil
	}
}

func (h *VoteExtHandler) computeOraclePrices(providerAgg *ProviderAggregator) (prices map[string]sdk.Dec, err error) {
	// Compute TVWAP based on candles or VWAP based on prices. For brevity and
	// demo purposes, we omit implementation.
	return prices, err
}

func (h *VoteExtHandler) verifyPrices(ctx sdk.Context, prices map[string]sdk.Dec) error {
	// Verify incoming prices from a validator are within a reasonable range based
	// on our own prices, i.e. h.computedPrices. For brevity and demo purposes, we
	// omit implementation.
	return nil
}
