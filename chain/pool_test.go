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
	good, err := Sign(key, to, 10, 1)
	if err != nil {
		t.Fatal(err)
	}

	badSig := good
	badSig.Sig = bytes.Clone(good.Sig)
	badSig.Sig[0] = 0
	pool := NewPool()
	if err := pool.Add(badSig, 100, 0); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("bad signature err = %v", err)
	}

	low, err := Sign(key, to, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(low, 100, 1); !errors.Is(err, ErrNonceTooLow) {
		t.Fatalf("low nonce err = %v", err)
	}

	poor, err := Sign(key, to, 50, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Add(poor, 40, 1); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("balance err = %v", err)
	}
}

func TestPoolOrdersBySenderThenNonce(t *testing.T) {
	var to Address
	to[19] = 2
	demo, err := Sign(demoKey(t), to, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	demoFirst, err := Sign(demoKey(t), to, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Sign(otherKey(), to, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewPool()
	for _, tx := range []Tx{demo, other, demoFirst} {
		if err := pool.Add(tx, 100, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.Add(demoFirst, 100, 0); !errors.Is(err, ErrNonceUsed) {
		t.Fatalf("duplicate err = %v", err)
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

func TestPoolCopiesSignatures(t *testing.T) {
	var to Address
	to[19] = 2
	tx, err := Sign(demoKey(t), to, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	caller := bytes.Clone(tx.Sig)
	tx.Sig = caller
	pool := NewPool()
	if err := pool.Add(tx, 100, 0); err != nil {
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
	first, err := Sign(demoKey(t), to, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(demoKey(t), to, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewPool()
	for _, tx := range []Tx{first, second} {
		if err := pool.Add(tx, 100, 0); err != nil {
			t.Fatal(err)
		}
	}
	sender, err := first.Sender()
	if err != nil {
		t.Fatal(err)
	}
	pool.Drop(sender, first.Nonce)
	if cap(pool.items) <= len(pool.items) {
		t.Fatal("drop left no spare slot to check")
	}
	for _, it := range pool.items[:cap(pool.items)][len(pool.items):] {
		if it.tx.Sig != nil || it.tx.Nonce != 0 || it.tx.Value != 0 {
			t.Fatal("dropped transaction is still reachable past the pool length")
		}
	}
	left := pool.Pending()
	if len(left) != 1 || left[0].Nonce != second.Nonce {
		t.Fatalf("remaining = %+v", left)
	}
}
