package chain

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

func funded(t *testing.T, balance, nonce uint64) (*State, Address) {
	t.Helper()
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
	st := NewState()
	st.Put(Account{Address: addr, Balance: balance, Nonce: nonce})
	return st, addr
}

func TestApplyTransferUpdatesNonceAndBalances(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var to Address
	to[19] = 9
	tx, err := Sign(demoKey(t), to, 40, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Get(from)
	if !ok || got.Balance != 100000-40-21000 || got.Nonce != 1 {
		t.Fatalf("sender = %+v ok=%v", got, ok)
	}
	recip, ok := st.Get(to)
	if !ok || recip.Balance != 40 || recip.Nonce != 0 {
		t.Fatalf("recipient = %+v ok=%v", recip, ok)
	}
}

func TestApplyZeroAddressIsTransferNotCreate(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var coin Address
	coin[19] = 3
	st.SetCoinbase(coin)
	tx, err := Sign(demoKey(t), Address{}, 10, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	recip, ok := st.Get(Address{})
	if !ok || recip.Balance != 10 || recip.Nonce != 0 || len(st.Code(Address{})) != 0 {
		t.Fatalf("zero address = %+v ok=%v", recip, ok)
	}
	got, _ := st.Get(from)
	if got.Balance != 100000-10-21000 || got.Nonce != 1 {
		t.Fatalf("sender = %+v", got)
	}
}

func TestApplySelfTransferKeepsBalance(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var coin Address
	coin[19] = 5
	st.SetCoinbase(coin)
	tx, err := Sign(demoKey(t), from, 5, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(from)
	if got.Balance != 100000-21000 || got.Nonce != 1 {
		t.Fatalf("self = %+v", got)
	}
	paid, ok := st.Get(coin)
	if !ok || paid.Balance != 21000 {
		t.Fatalf("coinbase = %+v ok=%v", paid, ok)
	}
}

func TestApplyRejectsWithoutMutating(t *testing.T) {
	var to Address
	to[19] = 3
	tests := []struct {
		name    string
		balance uint64
		tx      func() Tx
		want    error
	}{
		{"nonce", 100000, func() Tx {
			tx, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
			if err != nil {
				t.Fatal(err)
			}
			return tx
		}, ErrNonceMismatch},
		{"balance", 10, func() Tx {
			tx, err := Sign(demoKey(t), to, 11, 1, 21000, 1, ChainID)
			if err != nil {
				t.Fatal(err)
			}
			return tx
		}, ErrInsufficientBalance},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, from := funded(t, tt.balance, 1)
			before, _ := st.Get(from)
			_, err := Apply(st, tt.tx())
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v", err)
			}
			got, _ := st.Get(from)
			if got != before {
				t.Fatalf("state changed to %+v", got)
			}
		})
	}
}

func TestApplyRejectsRecipientOverflow(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var to Address
	to[19] = 4
	st.Put(Account{Address: to, Balance: math.MaxUint64 - 1, Nonce: 0})
	senderBefore, _ := st.Get(from)
	tx, err := Sign(demoKey(t), to, 2, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("err = %v", err)
	}
	got, _ := st.Get(to)
	if got.Balance != math.MaxUint64-1 || got.Nonce != 0 {
		t.Fatalf("recipient = %+v", got)
	}
	senderAfter, _ := st.Get(from)
	if senderAfter != senderBefore {
		t.Fatalf("sender changed to %+v", senderAfter)
	}
}

func TestApplyRejectsBadSignatureWithoutMutating(t *testing.T) {
	st, from := funded(t, 10, 0)
	var other Address
	other[19] = 8
	st.Put(Account{Address: other, Balance: 4, Nonce: 1})
	var to Address
	to[19] = 3
	tx, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	tx.Sig = bytes.Clone(tx.Sig)
	tx.Sig[0] = 0
	senderBefore, _ := st.Get(from)
	otherBefore, _ := st.Get(other)
	if _, err := Apply(st, tx); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("err = %v", err)
	}
	senderAfter, _ := st.Get(from)
	otherAfter, _ := st.Get(other)
	if senderAfter != senderBefore || otherAfter != otherBefore {
		t.Fatal("bad signature changed state")
	}
	if _, ok := st.Get(to); ok {
		t.Fatal("bad signature created the recipient")
	}
}

func TestApplyPaysCoinbase(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	var to, coin Address
	to[19] = 9
	coin[19] = 4
	st.SetCoinbase(coin)
	tx, err := Sign(demoKey(t), to, 10, 0, 21000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := Apply(st, tx)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.GasUsed != IntrinsicGas || receipt.Status != 1 {
		t.Fatalf("receipt = %+v", receipt)
	}
	got, ok := st.Get(from)
	if !ok || got.Balance != 1000000-10-42000 || got.Nonce != 1 {
		t.Fatalf("sender = %+v ok=%v", got, ok)
	}
	recip, ok := st.Get(to)
	if !ok || recip.Balance != 10 || recip.Nonce != 0 {
		t.Fatalf("recipient = %+v ok=%v", recip, ok)
	}
	miner, ok := st.Get(coin)
	if !ok || miner.Balance != 42000 || miner.Nonce != 0 {
		t.Fatalf("coinbase = %+v ok=%v", miner, ok)
	}

	wideState, wideFrom := funded(t, 1000000, 0)
	wideState.SetCoinbase(coin)
	wide, err := Sign(demoKey(t), to, 10, 0, 40000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err = Apply(wideState, wide)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.GasUsed != 21000 {
		t.Fatalf("wide gas = %d", receipt.GasUsed)
	}
	wideSender, _ := wideState.Get(wideFrom)
	if wideSender.Balance != 1000000-10-42000 {
		t.Fatalf("wide sender balance = %d", wideSender.Balance)
	}
	wideCoin, _ := wideState.Get(coin)
	if wideCoin.Balance != 42000 {
		t.Fatalf("wide coinbase balance = %d", wideCoin.Balance)
	}

	lowState, lowFrom := funded(t, 1000000, 0)
	before, _ := lowState.Get(lowFrom)
	root := lowState.Root()
	low, err := Sign(demoKey(t), to, 10, 0, 20999, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(lowState, low); !errors.Is(err, ErrIntrinsicGas) || err.Error() != "intrinsic gas" {
		t.Fatalf("err = %v", err)
	}
	after, _ := lowState.Get(lowFrom)
	if after != before || lowState.Root() != root {
		t.Fatalf("state changed to %+v", after)
	}
	if _, ok := lowState.Get(to); ok {
		t.Fatal("intrinsic gas created the recipient")
	}
	if _, ok := lowState.Get(coin); ok {
		t.Fatal("intrinsic gas created the coinbase")
	}
	if err := NewPool().Add(low, 1000000, 0); !errors.Is(err, ErrIntrinsicGas) {
		t.Fatalf("pool err = %v", err)
	}
}

func TestApplyRefundsFeeWhenSenderIsCoinbase(t *testing.T) {
	st, from := funded(t, 100000, 0)
	st.SetCoinbase(from)
	var to Address
	to[19] = 9
	tx, err := Sign(demoKey(t), to, 10, 0, 21000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(from)
	if got.Balance != 100000-10 || got.Nonce != 1 {
		t.Fatalf("sender coinbase = %+v", got)
	}
	recip, ok := st.Get(to)
	if !ok || recip.Balance != 10 {
		t.Fatalf("recipient = %+v ok=%v", recip, ok)
	}

	self, selfFrom := funded(t, 100000, 0)
	self.SetCoinbase(selfFrom)
	tx, err = Sign(demoKey(t), selfFrom, 5, 0, 21000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(self, tx); err != nil {
		t.Fatal(err)
	}
	got, _ = self.Get(selfFrom)
	if got.Balance != 100000 || got.Nonce != 1 {
		t.Fatalf("self coinbase = %+v", got)
	}
}

func TestApplyRejectsCoinbaseOverflow(t *testing.T) {
	st, from := funded(t, 100000, 0)
	var to, coin Address
	to[19] = 9
	coin[19] = 4
	st.SetCoinbase(coin)
	st.Put(Account{Address: coin, Balance: math.MaxUint64, Nonce: 0})
	senderBefore, _ := st.Get(from)
	root := st.Root()
	tx, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(st, tx); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("err = %v", err)
	}
	senderAfter, _ := st.Get(from)
	coinAfter, _ := st.Get(coin)
	if senderAfter != senderBefore || coinAfter.Balance != math.MaxUint64 || st.Root() != root {
		t.Fatal("coinbase overflow changed state")
	}
	if _, ok := st.Get(to); ok {
		t.Fatal("coinbase overflow created the recipient")
	}
}
