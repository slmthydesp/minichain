package chain

import (
	"bytes"
	"testing"
)

func TestForkChoicePrefersSmallerHashThenLongerChain(t *testing.T) {
	var leftCoin, rightCoin Address
	leftCoin[19] = 1
	rightCoin[19] = 2
	leftChain := New()
	rightChain := New()
	leftBlock, err := leftChain.Mine(leftCoin)
	if err != nil {
		t.Fatal(err)
	}
	rightBlock, err := rightChain.Mine(rightCoin)
	if err != nil {
		t.Fatal(err)
	}
	obs := New()
	if err := obs.Append(leftBlock); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(rightBlock); err != nil {
		t.Fatal(err)
	}
	leftHash := leftBlock.Hash()
	rightHash := rightBlock.Hash()
	want := leftHash
	if bytes.Compare(rightHash[:], leftHash[:]) < 0 {
		want = rightHash
	}
	if obs.Head().Hash() != want {
		t.Fatalf("head = %s want %s", obs.Head().Hash().Hex(), want.Hex())
	}
	if !obs.Has(leftBlock.Hash()) || !obs.Has(rightBlock.Hash()) {
		t.Fatal("a competing block was discarded")
	}

	extra, err := rightChain.Mine(rightCoin)
	if err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(extra); err != nil {
		t.Fatal(err)
	}
	if obs.Head().Hash() != extra.Hash() {
		t.Fatalf("head = %s want longer %s", obs.Head().Hash().Hex(), extra.Hash().Hex())
	}
	got := obs.BlocksAfter(0)
	if len(got) != 2 || got[0].Hash() != rightBlock.Hash() || got[1].Hash() != extra.Hash() {
		t.Fatalf("canonical blocks = %d", len(got))
	}
}

func TestReorgRestoresAbandonedTransfer(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 9
	tx, err := Sign(key, to, 10, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	var shortCoin, longCoin Address
	shortCoin[19] = 1
	longCoin[19] = 2

	short := New()
	if _, err := short.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	spent, err := short.Mine(shortCoin)
	if err != nil {
		t.Fatal(err)
	}
	if len(spent.Txs) != 1 {
		t.Fatalf("short block txs = %d", len(spent.Txs))
	}

	long := New()
	first, err := long.Mine(longCoin)
	if err != nil {
		t.Fatal(err)
	}
	second, err := long.Mine(longCoin)
	if err != nil {
		t.Fatal(err)
	}

	obs := New()
	if _, err := obs.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(spent); err != nil {
		t.Fatal(err)
	}
	if len(obs.Pool().Pending()) != 0 {
		t.Fatal("included transaction still pending")
	}
	if acct, _ := obs.Account(to); acct.Balance != 10 {
		t.Fatalf("balance = %d", acct.Balance)
	}
	if err := obs.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(second); err != nil {
		t.Fatal(err)
	}
	if obs.Head().Hash() != second.Hash() {
		t.Fatal("longer chain did not become head")
	}
	if len(obs.Pool().Pending()) != 1 {
		t.Fatalf("pending after reorg = %d", len(obs.Pool().Pending()))
	}
	if acct, _ := obs.Account(to); acct.Balance != 0 {
		t.Fatalf("balance after reorg = %d", acct.Balance)
	}
	if !obs.Has(spent.Hash()) {
		t.Fatal("abandoned block was dropped")
	}
}

func TestReorgDoesNotRestoreNonceSpentOnWinner(t *testing.T) {
	key := demoKey(t)
	var toA, toB Address
	toA[19] = 1
	toB[19] = 2
	txA, err := Sign(key, toA, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	txB, err := Sign(key, toB, 2, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	var shortCoin, longCoin Address
	shortCoin[19] = 3
	longCoin[19] = 4

	short := New()
	if _, err := short.AddTx(txA); err != nil {
		t.Fatal(err)
	}
	spent, err := short.Mine(shortCoin)
	if err != nil {
		t.Fatal(err)
	}
	long := New()
	if _, err := long.AddTx(txB); err != nil {
		t.Fatal(err)
	}
	first, err := long.Mine(longCoin)
	if err != nil {
		t.Fatal(err)
	}
	second, err := long.Mine(longCoin)
	if err != nil {
		t.Fatal(err)
	}

	obs := New()
	if _, err := obs.AddTx(txA); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(spent); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(second); err != nil {
		t.Fatal(err)
	}
	if len(obs.Pool().Pending()) != 0 {
		t.Fatal("nonce spent on the winning chain returned to the pool")
	}
	if acct, _ := obs.Account(toB); acct.Balance != 2 {
		t.Fatalf("winner recipient = %d", acct.Balance)
	}
}

func TestAppendDuplicateIsNoop(t *testing.T) {
	var coin Address
	coin[19] = 5
	block, err := New().Mine(coin)
	if err != nil {
		t.Fatal(err)
	}
	obs := New()
	if err := obs.Append(block); err != nil {
		t.Fatal(err)
	}
	if err := obs.Append(block); err != nil {
		t.Fatal(err)
	}
	if obs.Head().Height != 1 || obs.Head().Hash() != block.Hash() {
		t.Fatal("duplicate append changed the head")
	}
}

func TestAppendRejectsUnknownParent(t *testing.T) {
	var coin Address
	coin[19] = 6
	block, err := New().Mine(coin)
	if err != nil {
		t.Fatal(err)
	}
	block.ParentHash[0] ^= 0xff
	obs := New()
	if err := obs.Append(block); err == nil {
		t.Fatal("expected unknown parent")
	}
	if obs.Head().Height != 0 || obs.Has(block.Hash()) {
		t.Fatal("rejected block was stored")
	}
}
