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
governed by governance.

Each validator will have `ExtendVote` called. It will fetch and aggregate prices,
then it will compute prices using TWAP for example, persist them either ephemerally
or on disk, and finally encode them to be returned in `ResponseExtendVote`.

## VerifyVoteExtension

In `abci/vote_exts.go`, you'll find the `VoteExtHandler` handler type. This handler
implements the `VerifyVoteExtensionHandler` ABCI method. This method is called by
the CometBFT client when it verifies another validator's "extended" pre-commit
for a block proposal.

In the context of an Oracle, this is where the application would verify another
validator's computed oracle prices and compare the results to what it computed in
`ExtendVote`. Recall, CometBFT will call `VerifyVoteExtension` for every single
pre-commit it receives (but never for its own). Verification of another validator's
prices might include ensuring the price for each supported currency pair is within
a certain threshold to its own and that no unsupported currency pairs are included.

## PrepareProposal

## ProcessProposal
