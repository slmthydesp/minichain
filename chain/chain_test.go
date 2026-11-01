package chain

import (
	"bytes"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

func TestNewGenesisFundsDemoAccount(t *testing.T) {
	c := New()
	head := c.Head()
	if head.Height != 0 || head.ParentHash != (Hash{}) || len(head.Txs) != 0 {
		t.Fatalf("genesis = %+v", head)
	}
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	var addr Address
	copy(addr[:], raw[:])
	acct, ok := c.Account(addr)
	if !ok || acct.Balance != GenesisBalance || acct.Nonce != 0 {
		t.Fatalf("demo = %+v ok=%v", acct, ok)
	}
	st := NewState()
	st.Put(acct)
	if head.StateRoot != st.Root() {
		t.Fatalf("genesis root = %s", head.StateRoot.Hex())
	}
}

func TestNextNonceSkipsPending(t *testing.T) {
	c := New()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	var sender Address
	copy(sender[:], raw[:])
	if c.NextNonce(sender) != 0 {
		t.Fatal("empty next nonce")
	}
	var to Address
	to[19] = 1
	future, err := Sign(key, to, 1, 2, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(future); err != nil {
		t.Fatal(err)
	}
	if c.NextNonce(sender) != 0 {
		t.Fatal("gap should stay open")
	}
	first, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(first); err != nil {
		t.Fatal(err)
	}
	if c.NextNonce(sender) != 1 {
		t.Fatal("nonce 0 is taken")
	}
	second, err := Sign(key, to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(second); err != nil {
		t.Fatal(err)
	}
	if got := c.NextNonce(sender); got != 3 {
		t.Fatalf("next = %d", got)
	}
}

func TestMineAndAppend(t *testing.T) {
	c := New()
	parent := c.Head().Hash()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[19] = 8
	tx, err := Sign(key, to, 15, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if block.Height != 1 || block.ParentHash != parent || len(block.Txs) != 1 {
		t.Fatalf("mined = %+v", block)
	}
	if len(c.Pool().Pending()) != 0 {
		t.Fatal("mined transaction still pending")
	}
	acct, _ := c.Account(to)
	if acct.Balance != 15 {
		t.Fatalf("recipient balance = %d", acct.Balance)
	}

	follower := New()
	if err := follower.Append(block); err != nil {
		t.Fatal(err)
	}
	if follower.Head().Hash() != c.Head().Hash() || follower.Head().StateRoot != c.Head().StateRoot {
		t.Fatal("follower diverged")
	}

	badParent := block
	badParent.ParentHash[0] ^= 0x01
	if err := New().Append(badParent); err == nil {
		t.Fatal("expected bad parent to fail")
	}
	badRoot := block
	badRoot.StateRoot[0] ^= 0x01
	fresh := New()
	if err := fresh.Append(badRoot); err == nil {
		t.Fatal("expected bad root to fail")
	}
	if fresh.Head().Height != 0 {
		t.Fatal("rejected block was kept")
	}
}

func TestMineLeavesGappedNonce(t *testing.T) {
	c := New()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[0] = 1
	future, err := Sign(key, to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(future); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if block.Height != 1 || len(block.Txs) != 0 {
		t.Fatalf("empty block = height %d txs %d", block.Height, len(block.Txs))
	}
	if len(c.Pool().Pending()) != 1 {
		t.Fatal("gapped transaction was dropped")
	}
}

func TestAppendDropsIncludedTx(t *testing.T) {
	producer := New()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[1] = 2
	tx, err := Sign(key, to, 4, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := producer.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := producer.Mine(Address{}, producer.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	follower := New()
	if _, err := follower.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	if err := follower.Append(block); err != nil {
		t.Fatal(err)
	}
	if len(follower.Pool().Pending()) != 0 {
		t.Fatal("appended transaction still pending")
	}
	if err := follower.Append(block); err != nil {
		t.Fatal(err)
	}
	if follower.Head().Height != 1 || len(follower.Pool().Pending()) != 0 {
		t.Fatal("duplicate append changed the chain")
	}
}

func TestAppendRejectsSkippedHeight(t *testing.T) {
	c := New()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[19] = 8
	tx, err := Sign(key, to, 15, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}

	skipped := block
	skipped.Height = 2
	follower := New()
	if err := follower.Append(skipped); err == nil {
		t.Fatal("expected skipped height to fail")
	}
	if follower.Head().Height != 0 {
		t.Fatal("follower advanced past genesis")
	}
}

func TestMineStopsWhenBlockGasIsFull(t *testing.T) {
	c := New()
	demo := demoKey(t)
	other := otherKey()
	otherRaw, err := sig.KeyAddress(other)
	if err != nil {
		t.Fatal(err)
	}
	var otherAddr Address
	copy(otherAddr[:], otherRaw[:])
	demoRaw, err := sig.KeyAddress(demo)
	if err != nil {
		t.Fatal(err)
	}
	var demoAddr Address
	copy(demoAddr[:], demoRaw[:])

	fund, err := Sign(demo, otherAddr, 400000, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(fund); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mine(Address{}, c.Head().Time+1); err != nil {
		t.Fatal(err)
	}

	var sink Address
	sink[19] = 4
	earlierKey, earlierNonce := demo, uint64(1)
	laterKey, laterNonce := other, uint64(0)
	if bytes.Compare(otherAddr[:], demoAddr[:]) < 0 {
		earlierKey, earlierNonce = other, 0
		laterKey, laterNonce = demo, 1
	}
	first, err := Sign(earlierKey, sink, 1, earlierNonce, 60000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(earlierKey, sink, 1, earlierNonce+1, 50000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	third, err := Sign(laterKey, sink, 1, laterNonce, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range []Tx{second, third, first} {
		if _, err := c.AddTx(tx); err != nil {
			t.Fatal(err)
		}
	}
	var coin Address
	coin[19] = 3
	block, err := c.Mine(coin, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if block.GasLimit != BlockGasLimit {
		t.Fatalf("gas limit = %d", block.GasLimit)
	}
	if block.GasUsed != 42000 || len(block.Txs) != 2 {
		t.Fatalf("gas used = %d txs = %d", block.GasUsed, len(block.Txs))
	}
	pending := c.Pool().Pending()
	if len(pending) != 1 || pending[0].GasLimit != 50000 || pending[0].Nonce != earlierNonce+1 {
		t.Fatalf("pending = %+v", pending)
	}
	paid, ok := c.Account(coin)
	if !ok || paid.Balance != 42000 {
		t.Fatalf("coinbase = %+v ok=%v", paid, ok)
	}
}

func TestBlockHashBindsGas(t *testing.T) {
	base := Block{ParentHash: Hash{1}, StateRoot: Hash{2}, Coinbase: Address{19: 1}}
	limit := base
	limit.GasLimit = BlockGasLimit
	used := base
	used.GasUsed = IntrinsicGas
	tall := base
	tall.Height = 1
	late := base
	late.Time = 1
	if base.Hash() == limit.Hash() || base.Hash() == used.Hash() || base.Hash() == tall.Hash() || base.Hash() == late.Hash() {
		t.Fatal("hash ignored gas, height, or timestamp")
	}
}

func TestMineReceiptsRootMatchesTransactions(t *testing.T) {
	c := New()
	key := demoKey(t)
	var to Address
	to[19] = 9
	for nonce, value := range []uint64{4, 6} {
		tx, err := Sign(key, to, value, uint64(nonce), 21000, 1, ChainID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.AddTx(tx); err != nil {
			t.Fatal(err)
		}
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	recomputed := receiptsFromTransfers(block.Txs)
	if len(recomputed) != 2 || recomputed[0].CumulativeGasUsed != 21000 || recomputed[1].CumulativeGasUsed != 42000 {
		t.Fatalf("recomputed = %+v", recomputed)
	}
	if block.ReceiptsRoot != ReceiptsRoot(recomputed) {
		t.Fatalf("receipts root = %s", block.ReceiptsRoot.Hex())
	}
	if block.TransactionsRoot != TransactionsRoot(block.Txs) {
		t.Fatalf("transactions root = %s", block.TransactionsRoot.Hex())
	}
	withoutTxs := block
	withoutTxs.Txs = nil
	if withoutTxs.Hash() != block.Hash() {
		t.Fatal("hash included the raw transactions")
	}
	flipped := block
	flipped.ReceiptsRoot[0] ^= 0x01
	if flipped.Hash() == block.Hash() {
		t.Fatal("hash ignored receipts root")
	}
	withoutList := block
	withoutList.Receipts = nil
	if withoutList.Hash() != block.Hash() {
		t.Fatal("hash included the receipt list")
	}
	if len(block.Receipts) != len(recomputed) {
		t.Fatalf("receipts = %+v", block.Receipts)
	}
	for i := range recomputed {
		got, want := block.Receipts[i], recomputed[i]
		if got.Status != want.Status || got.GasUsed != want.GasUsed || got.CumulativeGasUsed != want.CumulativeGasUsed || len(got.Logs) != len(want.Logs) {
			t.Fatalf("receipt %d = %+v", i, got)
		}
	}
}

func TestAppendRejectsBadReceiptsRoot(t *testing.T) {
	c := New()
	key := demoKey(t)
	var to Address
	to[19] = 8
	tx, err := Sign(key, to, 15, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	block.ReceiptsRoot[0] ^= 0x01
	follower := New()
	if err := follower.Append(block); err == nil {
		t.Fatal("expected bad receipts root to fail")
	}
	if follower.Head().Height != 0 {
		t.Fatal("follower advanced past genesis")
	}
	if _, ok := follower.Account(to); ok {
		t.Fatal("rejected block changed state")
	}
}

func TestMineRejectsTimestampNotAfterParent(t *testing.T) {
	c := New()
	if _, err := c.Mine(Address{}, 0); err == nil || err.Error() != "bad timestamp" {
		t.Fatalf("timestamp err = %v", err)
	}
	if _, err := c.Mine(Address{}, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mine(Address{}, 1); err == nil || err.Error() != "bad timestamp" {
		t.Fatalf("equal timestamp err = %v", err)
	}
}

func TestAppendChecksTimestampGasLimitAndGasUsed(t *testing.T) {
	c := New()
	block, err := c.Mine(Address{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	follower := New()
	early := block
	early.Time = 0
	if err := follower.Append(early); err == nil || err.Error() != "bad timestamp" {
		t.Fatalf("timestamp err = %v", err)
	}
	limited := block
	limited.GasLimit = 1
	if err := follower.Append(limited); err == nil || err.Error() != "bad gas limit" {
		t.Fatalf("gas limit err = %v", err)
	}
	used := block
	used.GasUsed = 1
	if err := follower.Append(used); err == nil || err.Error() != "bad gas used" {
		t.Fatalf("gas used err = %v", err)
	}
	if follower.Head().Height != 0 {
		t.Fatal("rejected block advanced the head")
	}
	if err := follower.Append(block); err != nil {
		t.Fatal(err)
	}
}

func TestAppendRejectsGasLimitSumOverBlock(t *testing.T) {
	c := New()
	var to Address
	to[19] = 4
	first, err := Sign(demoKey(t), to, 1, 0, 60000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(demoKey(t), to, 1, 1, 60000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	parent := c.Head()
	block := Block{
		Height:           parent.Height + 1,
		ParentHash:       parent.Hash(),
		GasLimit:         BlockGasLimit,
		GasUsed:          IntrinsicGas * 2,
		Time:             parent.Time + 1,
		Txs:              []Tx{first, second},
		TransactionsRoot: TransactionsRoot([]Tx{first, second}),
	}
	if err := c.Append(block); err == nil || err.Error() != "block gas exceeded" {
		t.Fatalf("err = %v", err)
	}
	if c.Head().Height != 0 || c.Has(block.Hash()) {
		t.Fatal("over-gas block was stored")
	}
}

func TestMinePacksHigherGasPriceFirst(t *testing.T) {
	c := New()
	demo := demoKey(t)
	other := otherKey()
	demoRaw, err := sig.KeyAddress(demo)
	if err != nil {
		t.Fatal(err)
	}
	otherRaw, err := sig.KeyAddress(other)
	if err != nil {
		t.Fatal(err)
	}
	var demoAddr, otherAddr Address
	copy(demoAddr[:], demoRaw[:])
	copy(otherAddr[:], otherRaw[:])
	fund, err := Sign(demo, otherAddr, 400000, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(fund); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mine(Address{}, c.Head().Time+1); err != nil {
		t.Fatal(err)
	}
	cheapKey, priceyKey := demo, other
	cheapNonce, priceyNonce := uint64(1), uint64(0)
	if bytes.Compare(demoAddr[:], otherAddr[:]) > 0 {
		priceyKey, cheapKey = demo, other
		priceyNonce, cheapNonce = 1, 0
	}
	var sink Address
	sink[19] = 7
	cheap, err := Sign(cheapKey, sink, 1, cheapNonce, 60000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	pricey, err := Sign(priceyKey, sink, 1, priceyNonce, 60000, 5, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range []Tx{cheap, pricey} {
		if _, err := c.AddTx(tx); err != nil {
			t.Fatal(err)
		}
	}
	block, err := c.Mine(Address{19: 3}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Txs) != 1 || block.Txs[0].GasPrice != 5 {
		t.Fatalf("packed %+v", block.Txs)
	}
}

func receiptsFromTransfers(txs []Tx) []Receipt {
	out := make([]Receipt, len(txs))
	var cumulative uint64
	for i := range txs {
		cumulative += IntrinsicGas
		out[i] = Receipt{Status: 1, GasUsed: IntrinsicGas, CumulativeGasUsed: cumulative}
	}
	return out
}
