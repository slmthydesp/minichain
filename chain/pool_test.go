package chain

import (
	"bytes"
	"errors"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

func demoKey(t *testing.T) []byte {
	t.Helper()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func otherKey() []byte {
	key := make([]byte, 32)
	key[31] = 1
	return key
}

func TestPoolRejectsBadSignatureNonceAndBalance(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 1
	good, err := Sign(key, to, 10, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}

	badSig := good
	badSig.Sig = bytes.Clone(good.Sig)
	badSig.Sig[0] = 0
	pool := NewPool()
	if err := pool.Add(badSig, 100000, 0); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("bad signature err = %v", err)
	}

	low, err := Sign(key, to, 10, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(low, 100000, 1); !errors.Is(err, ErrNonceTooLow) {
		t.Fatalf("low nonce err = %v", err)
	}

	poor, err := Sign(key, to, 50, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(poor, 40, 1); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("balance err = %v", err)
	}

	fee, err := Sign(key, to, 10, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(fee, 21009, 1); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("fee err = %v", err)
	}
}

func TestPoolRejectsFeeOverflow(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 1
	product, err := Sign(key, to, 1, 0, ^uint64(0), 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewPool().Add(product, ^uint64(0), 0); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("product err = %v", err)
	}
	price := ^uint64(0) / IntrinsicGas
	reserved := IntrinsicGas * price
	sum, err := Sign(key, to, ^uint64(0)-reserved+1, 0, IntrinsicGas, price, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewPool().Add(sum, ^uint64(0), 0); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("sum err = %v", err)
	}
	st, _ := funded(t, ^uint64(0), 0)
	if _, err := Apply(st, sum); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("apply err = %v", err)
	}
}

func TestPoolOrdersBySenderThenNonce(t *testing.T) {
	var to Address
	to[19] = 2
	demo, err := Sign(demoKey(t), to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	demoFirst, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Sign(otherKey(), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewPool()
	for _, tx := range []Tx{demo, other, demoFirst} {
		if err := pool.Add(tx, 100000, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.Add(demoFirst, 100000, 0); !errors.Is(err, ErrReplacementUnderpriced) {
		t.Fatalf("replacement err = %v", err)
	}
	got := pool.Pending()
	if len(got) != 3 {
		t.Fatalf("pending = %d", len(got))
	}
	senders := make([]Address, len(got))
	for i, tx := range got {
		senders[i], err = tx.Sender()
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			cmp := bytes.Compare(senders[i-1][:], senders[i][:])
			if cmp > 0 || (cmp == 0 && got[i-1].Nonce > tx.Nonce) {
				t.Fatalf("order broken at %d", i)
			}
		}
	}
	droppedSender, droppedNonce := senders[0], got[0].Nonce
	pool.Drop(droppedSender, droppedNonce)
	left := pool.Pending()
	if len(left) != 2 {
		t.Fatalf("after drop pending = %d", len(left))
	}
	for _, tx := range left {
		sender, err := tx.Sender()
		if err != nil {
			t.Fatal(err)
		}
		if sender == droppedSender && tx.Nonce == droppedNonce {
			t.Fatal("dropped transaction is still pending")
		}
	}
}

func TestPoolReplacesUnderNonceWhenGasPriceIsHigher(t *testing.T) {
	var to Address
	to[19] = 2
	key := demoKey(t)
	cheap, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	pricey, err := Sign(key, to, 1, 0, 21000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewPool()
	if err := pool.Add(cheap, 100000, 0); err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(pricey, 100000, 0); err != nil {
		t.Fatalf("replace err = %v", err)
	}
	got := pool.Pending()
	if len(got) != 1 || got[0].GasPrice != pricey.GasPrice || !bytes.Equal(got[0].Sig, pricey.Sig) {
		t.Fatalf("pending = %+v", got)
	}
	if err := pool.Add(cheap, 100000, 0); !errors.Is(err, ErrReplacementUnderpriced) {
		t.Fatalf("underpriced err = %v", err)
	}
	got = pool.Pending()
	if len(got) != 1 || got[0].GasPrice != pricey.GasPrice || !bytes.Equal(got[0].Sig, pricey.Sig) {
		t.Fatalf("after underpriced pending = %+v", got)
	}
}

func TestPoolCopiesSignatures(t *testing.T) {
	var to Address
	to[19] = 2
	tx, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	caller := bytes.Clone(tx.Sig)
	tx.Sig = caller
	pool := NewPool()
	if err := pool.Add(tx, 100000, 0); err != nil {
		t.Fatal(err)
	}
	caller[0] ^= 0xff
	stored := pool.Pending()
	if len(stored) != 1 || bytes.Equal(stored[0].Sig, caller) {
		t.Fatal("pool kept the caller's signature slice")
	}
	stored[0].Sig[0] ^= 0xff
	again := pool.Pending()
	if bytes.Equal(again[0].Sig, stored[0].Sig) {
		t.Fatal("pending returned the pool's signature slice")
	}
}

func TestDropClearsDiscardedSlots(t *testing.T) {
	var to Address
	to[19] = 2
	first, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(demoKey(t), to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewPool()
	for _, tx := range []Tx{first, second} {
		if err := pool.Add(tx, 100000, 0); err != nil {
			t.Fatal(err)
		}
	}
	sender, err := first.Sender()
	if err != nil {
		t.Fatal(err)
	}
	pool.Drop(sender, first.Nonce)
	if _, ok := pool.bySender[sender][first.Nonce]; ok {
		t.Fatal("dropped nonce is still stored")
	}
	left := pool.Pending()
	if len(left) != 1 || left[0].Nonce != second.Nonce {
		t.Fatalf("remaining = %+v", left)
	}
}

func TestPoolReservesBalanceForOtherPendingTxs(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 2
	first, err := Sign(key, to, 979000, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(key, to, 979000, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 这个例子把余额钉在 1000000，用来卡住两笔预留。创世余额是 1 个币，放得下这两笔。
	const balance uint64 = 1000000
	pool := NewPool()
	if err := pool.Add(first, balance, 0); err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(second, balance, 0); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("second err = %v", err)
	}
	if len(pool.Pending()) != 1 {
		t.Fatal("over-balance transaction was stored")
	}
	one, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Sign(key, to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	affordable := NewPool()
	if err := affordable.Add(one, balance, 0); err != nil {
		t.Fatal(err)
	}
	if err := affordable.Add(two, balance, 0); err != nil {
		t.Fatal(err)
	}
}

func TestPoolReplacementMustStillFitWithSiblings(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 2
	cheap, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := Sign(key, to, 500000, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	tooBig, err := Sign(key, to, 500000, 0, 21000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 替换后的预留加上同账户另一笔，会超过这个 1000000。创世余额放得下。
	const balance uint64 = 1000000
	pool := NewPool()
	if err := pool.Add(cheap, balance, 0); err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(sibling, balance, 0); err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(tooBig, balance, 0); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("replacement err = %v", err)
	}
	pending := pool.Pending()
	if len(pending) != 2 || pending[0].GasPrice != 1 || pending[0].Nonce != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}
