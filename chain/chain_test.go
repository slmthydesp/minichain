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
	block, err := c.Mine(Address{})
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
	block, err := c.Mine(Address{})
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
	block, err := producer.Mine(Address{})
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
	block, err := c.Mine(Address{})
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

	fund, err := Sign(demo, otherAddr, 100000, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(fund); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mine(Address{}); err != nil {
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
	block, err := c.Mine(coin)
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
	if base.Hash() == limit.Hash() || base.Hash() == used.Hash() {
		t.Fatal("hash ignored gas limit or gas used")
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
	block, err := c.Mine(Address{})
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
		if block.Receipts[i] != recomputed[i] {
			t.Fatalf("receipt %d = %+v", i, block.Receipts[i])
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
	block, err := c.Mine(Address{})
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

func receiptsFromTransfers(txs []Tx) []Receipt {
	out := make([]Receipt, len(txs))
	var cumulative uint64
	for i := range txs {
		cumulative += IntrinsicGas
		out[i] = Receipt{Status: 1, GasUsed: IntrinsicGas, CumulativeGasUsed: cumulative}
	}
	return out
}
