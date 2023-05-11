# ABCI++ Oracle Demo

This repository demonstrates an elementary example of how to implement an in-protocol
Oracle leveraging ABCI++. Specifically, there are four ABCI methods that make this
possible -- `ExtendVote`, `VerifyVoteExtension`, `PrepareProposal` and `ProcessProposal`.
In the context of an Oracle, we will go into each of these methods in detail below.

## ExtendVote

In `abci/vote_exts.go`, you'll find the `VoteExtHandler` handler type. This handler
implements the `ExtendVote` ABCI method. This method is called by the CometBFT
client when it "extends" a pre-commit for a block proposal.

In the context of an Oracle, this is where the application would fetch prices
for a supported list of currency pairs. In terms of which pairs are supported and
what providers are used is up to the application. Since we're implementing an
in-process Oracle, it naturally makes sense for these to exist on-chain to be
governed by governance. Note, `ExtendVote` does NOT need to be deterministic.

Each validator will have `ExtendVote` called. It will fetch and aggregate prices,
then it will compute prices using TWAP for example, persist them either ephemerally
or on disk, and finally encode them to be returned in `ResponseExtendVote`.

## VerifyVoteExtension

In `abci/vote_exts.go`, you'll find the `VoteExtHandler` handler type. This handler
implements the `VerifyVoteExtensionHandler` ABCI method. This method is called by
the CometBFT client when it verifies another validator's "extended" pre-commit
for a block proposal.

In the context of an Oracle, this is where the application would verify another
validator's computed oracle prices. Recall, CometBFT will call `VerifyVoteExtension`
for every single pre-commit it receives (but never for its own). Verification of
another validator's prices might include ensuring that no unsupported currency
pairs are included and that each price is a valid price. Note, `VerifyVoteExtension`
MUST be deterministic and thus any verification must be deterministic as well.

## PrepareProposal

In `abci/proposal.go`, you'll find the `ProposalHandler` handler type. This handler
implements the `PrepareProposalHandler` ABCI method. This method is called by the
CometBFT client when a new block proposal is being prepared.

In the context of an Oracle, this is where the proposer would receive the agreed
upon vote extensions with their signatures. The proposer would then compute the
stake-weighted average of all the supported/required currency pairs. It would
then include the resulting stake-weighted average prices along with all the vote
extensions it used to compute them in a `StakeWeightedPrices` structure. The
proposer would then encode this structure and inject it into the block proposal,
essentially treating it as a fake transaction. The remainder of the block proposal
can proceed at the application's discretion, e.g. executing POB and normal transaction
inclusion.

## ProcessProposal

In `abci/proposal.go`, you'll find the `ProposalHandler` handler type. This handler
implements the `ProcessProposalHandler` ABCI method. This method is called by the
CometBFT client when a validator needs to process a block proposal.

In the context of Oracle, this is where the application would verify the vote
extensions and the stake-weighted prices included in the block proposal. It would
ensure that the vote extensions are valid and that the stake-weighted prices are
what they should be by calculating them itself. If the stake-weighted prices are
valid, it will update state to persist the oracle prices to be used for the block
and proceed to process the remainder of the proposal.
