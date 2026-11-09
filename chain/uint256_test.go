package chain

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
)

func TestUint256AddSubAndRLP(t *testing.T) {
	sum, overflow := U256(40).Add(U256(2))
	if overflow || sum != U256(42) {
		t.Fatalf("sum=%s overflow=%v", sum, overflow)
	}
	carried, overflow := U256(math.MaxUint64).Add(U256(1))
	var wantCarry Uint256
	wantCarry[23] = 1
	if overflow || carried != wantCarry {
		t.Fatalf("carry=%x overflow=%v", carried, overflow)
	}
	_, overflow = MaxUint256().Add(U256(1))
	if !overflow {
		t.Fatal("expected overflow")
	}
	borrow, under := wantCarry.Sub(U256(1))
	if under || borrow != U256(math.MaxUint64) {
		t.Fatalf("borrow=%x under=%v", borrow, under)
	}
	diff, under := U256(42).Sub(U256(2))
	if under || diff != U256(40) {
		t.Fatalf("diff=%s under=%v", diff, under)
	}
	if _, under = U256(1).Sub(U256(2)); !under {
		t.Fatal("expected underflow")
	}
	for _, n := range []uint64{0, 1, 127, 128, 255, 256, math.MaxUint64} {
		if !bytes.Equal(U256(n).rlp(), rlpUint(n)) {
			t.Fatalf("rlp %d = %x", n, U256(n).rlp())
		}
	}
	var wide Uint256
	wide[23] = 1
	raw, err := json.Marshal(wide)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `"18446744073709551616"` {
		t.Fatalf("json = %s", raw)
	}
	var back Uint256
	if err := json.Unmarshal(raw, &back); err != nil || back != wide {
		t.Fatalf("decoded %s err=%v", back, err)
	}
	small, err := json.Marshal(U256(20))
	if err != nil || string(small) != "20" {
		t.Fatalf("small json = %s err=%v", small, err)
	}
}

func TestWideValueTransfer(t *testing.T) {
	var value Uint256
	value[23] = 1
	st, from := funded(t, 0, 0)
	bal, _ := value.Add(U256(21000))
	st.SetAccount(Account{Address: from, Balance: bal})
	var to Address
	to[19] = 9
	tx, err := SignAmount(demoKey(t), to, value, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	flipped := tx
	flipped.Value[0] ^= 1
	if got, err := flipped.Sender(); err == nil && got == from {
		t.Fatal("high value byte was not covered by the signature")
	}
	if _, err := Apply(st, tx); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetAccount(to)
	if got.Balance != value {
		t.Fatalf("recipient = %s", got.Balance)
	}
}
