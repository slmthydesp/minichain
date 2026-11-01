package chain

import (
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

func initOf(runtime []byte) []byte {
	n := byte(len(runtime))
	pref := []byte{0x60, n, 0x60, 0x0c, 0x60, 0x00, 0x39, 0x60, n, 0x60, 0x00, 0xf3}
	return append(pref, runtime...)
}

func demoAddr(t *testing.T) Address {
	t.Helper()
	raw, err := sig.KeyAddress(demoKey(t))
	if err != nil {
		t.Fatal(err)
	}
	var addr Address
	copy(addr[:], raw[:])
	return addr
}

func mustApply(t *testing.T, st *State, tx Tx) Receipt {
	t.Helper()
	receipt, err := Apply(st, tx)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestWordArithmeticStoresDifference(t *testing.T) {
	st := NewState()
	var addr Address
	addr[19] = 1
	code := []byte{0x60, 0x01, 0x60, 0x03, 0x03, 0x60, 0x00, 0x55, 0x00}
	steps := maxSteps
	_, _, _, kind := runCode(st, addr, Address{}, nil, code, 100000, 1, &steps)
	if kind != execOK {
		t.Fatalf("kind = %d", kind)
	}
	if st.Storage(addr, Hash{}) != hashFromUint64(2) {
		t.Fatalf("storage = %x", st.Storage(addr, Hash{}))
	}
}

func TestIntrinsicGasCountsCalldata(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 2
	low, err := SignCall(key, to, 1, 0, 21019, 1, ChainID, []byte{0x00, 0x01})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(NewState(), low); err == nil {
		t.Fatal("expected intrinsic gas")
	}
	ok, err := SignCall(key, to, 1, 0, 21020, 1, ChainID, []byte{0x00, 0x01})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := funded(t, 100000, 0)
	receipt := mustApply(t, st, ok)
	if receipt.Status != 1 || receipt.GasUsed != 21020 {
		t.Fatalf("receipt = %+v", receipt)
	}
}

func TestDeployAndCallStoresValue(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	key := demoKey(t)
	runtime := []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x00}
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	receipt := mustApply(t, st, create)
	if receipt.Status != 1 {
		t.Fatalf("create status = %d gas = %d", receipt.Status, receipt.GasUsed)
	}
	addr := CreateAddress(from, 0)
	if string(st.Code(addr)) != string(runtime) {
		t.Fatalf("code = %x", st.Code(addr))
	}
	call, err := Sign(key, addr, 0, 1, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt = mustApply(t, st, call)
	if receipt.Status != 1 {
		t.Fatalf("call status = %d", receipt.Status)
	}
	if st.Storage(addr, Hash{}) != hashFromUint64(1) {
		t.Fatalf("storage = %x", st.Storage(addr, Hash{}))
	}
	if root := st.Root(); root == emptyTrieRoot() {
		t.Fatal("state root ignored code and storage")
	}
}

func TestCalldataIsStored(t *testing.T) {
	st, _ := funded(t, 1000000, 0)
	key := demoKey(t)
	runtime := []byte{0x60, 0x00, 0x35, 0x60, 0x00, 0x55, 0x00}
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, create)
	addr := CreateAddress(demoAddr(t), 0)
	call, err := SignCall(key, addr, 0, 1, 40000, 1, ChainID, []byte{0x2a})
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, call)
	got := st.Storage(addr, Hash{})
	if got[0] != 0x2a {
		t.Fatalf("storage = %x", got)
	}
}

func TestRevertDropsValueAndKeepsFee(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	key := demoKey(t)
	runtime := []byte{0x60, 0x00, 0x60, 0x00, 0xfd}
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, create)
	addr := CreateAddress(from, 0)
	before, _ := st.Get(from)
	call, err := Sign(key, addr, 10, 1, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt := mustApply(t, st, call)
	if receipt.Status != 0 || len(receipt.Logs) != 0 {
		t.Fatalf("receipt = %+v", receipt)
	}
	got, _ := st.Get(from)
	if got.Nonce != before.Nonce+1 {
		t.Fatalf("nonce = %d", got.Nonce)
	}
	if got.Balance != before.Balance-receipt.GasUsed {
		t.Fatalf("balance = %d want %d", got.Balance, before.Balance-receipt.GasUsed)
	}
	recip, _ := st.Get(addr)
	if recip.Balance != 0 {
		t.Fatalf("contract balance = %d", recip.Balance)
	}
}

func TestLogKeptOnSuccessAndDroppedOnRevert(t *testing.T) {
	st, _ := funded(t, 1000000, 0)
	key := demoKey(t)
	runtime := []byte{0x60, 0xab, 0x60, 0x00, 0x60, 0x00, 0xa1, 0x00}
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, create)
	addr := CreateAddress(demoAddr(t), 0)
	call, err := Sign(key, addr, 0, 1, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt := mustApply(t, st, call)
	if receipt.Status != 1 || len(receipt.Logs) != 1 || receipt.Logs[0].Address != addr {
		t.Fatalf("receipt = %+v", receipt)
	}
	if receipt.Logs[0].Topics[0] != hashFromUint64(0xab) {
		t.Fatalf("topic = %x", receipt.Logs[0].Topics[0])
	}
	plain := receipt
	plain.Logs = nil
	if ReceiptsRoot([]Receipt{receipt}) == ReceiptsRoot([]Receipt{plain}) {
		t.Fatal("receipts root ignored logs")
	}

	revertCode := append(append([]byte{}, runtime[:len(runtime)-1]...), 0x60, 0x00, 0x60, 0x00, 0xfd)
	st2, _ := funded(t, 1000000, 0)
	create, err = SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(revertCode))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st2, create)
	call, err = Sign(key, CreateAddress(demoAddr(t), 0), 0, 1, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt = mustApply(t, st2, call)
	if receipt.Status != 0 || len(receipt.Logs) != 0 {
		t.Fatalf("reverted receipt = %+v", receipt)
	}
}

func TestCallRunsCallee(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	key := demoKey(t)
	calleeCode := []byte{0x60, 0x07, 0x60, 0x00, 0x55, 0x00}
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(calleeCode))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, create)
	callee := CreateAddress(from, 0)
	callerCode := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x73}
	callerCode = append(callerCode, callee[:]...)
	callerCode = append(callerCode, 0x61, 0x4e, 0x20, 0xf1, 0x00)
	create, err = SignCreate(key, 0, 1, 90000, 1, ChainID, initOf(callerCode))
	if err != nil {
		t.Fatal(err)
	}
	if receipt := mustApply(t, st, create); receipt.Status != 1 {
		t.Fatalf("caller create = %+v", receipt)
	}
	caller := CreateAddress(from, 1)
	call, err := Sign(key, caller, 0, 2, 80000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt := mustApply(t, st, call); receipt.Status != 1 {
		t.Fatalf("call = %+v", receipt)
	}
	if st.Storage(callee, Hash{}) != hashFromUint64(7) {
		t.Fatalf("callee storage = %x", st.Storage(callee, Hash{}))
	}
}

func TestMineIncludesRevertedTransaction(t *testing.T) {
	c := New()
	key := demoKey(t)
	runtime := []byte{0x60, 0x00, 0x60, 0x00, 0xfd}
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(create); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mine(Address{}, c.Head().Time+1); err != nil {
		t.Fatal(err)
	}
	addr := CreateAddress(demoAddr(t), 0)
	call, err := Sign(key, addr, 10, 1, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(call); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Txs) != 1 || block.Receipts[0].Status != 0 {
		t.Fatalf("block txs=%d receipt=%+v", len(block.Txs), block.Receipts)
	}
	acct, _ := c.Account(demoAddr(t))
	if acct.Nonce != 2 {
		t.Fatalf("nonce = %d", acct.Nonce)
	}
}
