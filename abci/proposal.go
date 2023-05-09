package abci

import (
	"encoding/json"
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/libs/log"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/skip-mev/vote-ext-oracle-demo/keepers"
)

// StakeWeightedPrices defines the structure a proposer should use to calculate
// and submit the stake-weighted prices for a given set of supported currency
// pairs, in addition to the vote extensions used to calculate them. This is so
// validators can verify the proposer's calculations.
type StakeWeightedPrices struct {
	StakeWeightedPrices map[string]sdk.Dec
	VoteExtensions      []abci.ExtendedVoteInfo
}

type ProposalHandler struct {
	logger           log.Logger
	FauxOracleKeeper keepers.FauxOracleKeeper
	// FauxStakingKeeper keepers.FauxStakingKeeper
}

func (h *ProposalHandler) PrepareProposal() sdk.PrepareProposalHandler {
	return func(ctx sdk.Context, req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
		requiredPairs := h.FauxOracleKeeper.GetSupportedPairs(ctx)
		stakeWeightedPrices := make(map[string]sdk.Dec, len(requiredPairs)) // base -> average stake-weighted price
		for _, pair := range requiredPairs {
			stakeWeightedPrices[pair.Base] = sdk.ZeroDec()
		}

		var totalStake int64
		for _, v := range req.LocalLastCommit.Votes {
			var voteExt OracleVoteExtension

			if err := json.Unmarshal(v.VoteExtension, &voteExt); err != nil {
				h.logger.Error("failed to decode vote extension", "err", err, "validator", fmt.Sprintf("%x", v.Validator.Address))
				// NOTE: In SDK v0.48.x, we'd return an error here.
				return abci.ResponsePrepareProposal{}
			}

			totalStake += v.Validator.Power

			// Compute stake-weighted average of prices for each supported pair, i.e.
			// (P1)(W1) + (P2)(W2) + ... + (Pn)(Wn) / (W1 + W2 + ... + Wn)
			//
			// NOTE: These are the prices computed at the PREVIOUS height, i.e. H-1
			for base, price := range voteExt.Prices {
				// Only compute stake-weighted average for supported pairs.
				//
				// NOTE: VerifyVoteExtension should be sufficient to ensure that only
				// supported pairs are supplied, but we add this here for demo purposes.
				if _, ok := stakeWeightedPrices[base]; ok {
					stakeWeightedPrices[base] = stakeWeightedPrices[base].Add(price.MulInt64(v.Validator.Power))
				}
			}
		}

		// finalize average by dividing by total stake, i.e. total weights
		for base, price := range stakeWeightedPrices {
			stakeWeightedPrices[base] = price.QuoInt64(totalStake)
		}

		injectedTx := StakeWeightedPrices{
			StakeWeightedPrices: stakeWeightedPrices,
			VoteExtensions:      req.LocalLastCommit.Votes,
		}

		// NOTE: We use stdlib JSON encoding, but an application may choose to use
		// a performant mechanism. This is for demo purposes only.
		bz, err := json.Marshal(injectedTx)
		if err != nil {
			// NOTE: In SDK v0.48.x, we'd return an error here.
			return abci.ResponsePrepareProposal{}
		}

		var proposalTxs [][]byte

		// Inject a "fake" tx into the proposal s.t. validators can decode, verify,
		// and store the canonical stake-weighted average prices.
		proposalTxs = append(proposalTxs, bz)

		// proceed with normal block proposal construction, e.g. POB, normal txs, etc...

		return abci.ResponsePrepareProposal{
			Txs: proposalTxs,
		}
	}
}

func (h *ProposalHandler) ProcessProposal() sdk.ProcessProposalHandler {
	return func(ctx sdk.Context, req abci.RequestProcessProposal) abci.ResponseProcessProposal {
		// XXX: Call ValidateVoteExtensions once 0.48.x is released to verify vote
		// extension signatures and that 2/3 of the voting power is present.
		//
		// baseapp.ValidateVoteExtensions(...)
		panic("not implemented")
	}
}
