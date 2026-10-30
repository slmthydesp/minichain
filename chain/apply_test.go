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
	st, from := funded(t, 1000, 0)
	var to Address
	to[19] = 9
	tx, err := Sign(demoKey(t), to, 40, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Get(from)
	if !ok || got.Balance != 960 || got.Nonce != 1 {
		t.Fatalf("sender = %+v ok=%v", got, ok)
	}
	recip, ok := st.Get(to)
	if !ok || recip.Balance != 40 || recip.Nonce != 0 {
		t.Fatalf("recipient = %+v ok=%v", recip, ok)
	}
}

func TestApplySelfTransferKeepsBalance(t *testing.T) {
	st, from := funded(t, 100, 0)
	tx, err := Sign(demoKey(t), from, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(from)
	if got.Balance != 100 || got.Nonce != 1 {
		t.Fatalf("self = %+v", got)
	}
}

func TestApplyRejectsWithoutMutating(t *testing.T) {
	st, from := funded(t, 10, 1)
	var to Address
	to[19] = 3
	before, _ := st.Get(from)

	tests := []struct {
		name string
		tx   func() Tx
		want error
	}{
		{"nonce", func() Tx {
			tx, err := Sign(demoKey(t), to, 1, 0)
			if err != nil {
				t.Fatal(err)
			}
			return tx
		}, ErrNonceMismatch},
		{"balance", func() Tx {
			tx, err := Sign(demoKey(t), to, 11, 1)
			if err != nil {
				t.Fatal(err)
			}
			return tx
		}, ErrInsufficientBalance},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Apply(st, tt.tx())
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
	st, from := funded(t, 10, 0)
	var to Address
	to[19] = 4
	st.Put(Account{Address: to, Balance: math.MaxUint64 - 1, Nonce: 0})
	senderBefore, _ := st.Get(from)
	tx, err := Sign(demoKey(t), to, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(st, tx); !errors.Is(err, ErrBalanceOverflow) {
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
	tx, err := Sign(demoKey(t), to, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	tx.Sig = bytes.Clone(tx.Sig)
	tx.Sig[0] = 0
	senderBefore, _ := st.Get(from)
	otherBefore, _ := st.Get(other)
	if err := Apply(st, tx); !errors.Is(err, ErrBadSignature) {
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
