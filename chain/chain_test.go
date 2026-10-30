package chain

import (
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
