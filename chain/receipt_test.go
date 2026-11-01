package chain

import (
	"encoding/binary"
	"testing"
)

func TestEmptyReceiptsRoot(t *testing.T) {
	root := ReceiptsRoot(nil)
	if root != emptyTrieRoot() {
		t.Fatalf("empty root = %s", root.Hex())
	}
	if ReceiptsRoot([]Receipt{}) != root {
		t.Fatal("empty slice root differed from nil")
	}
}

func TestTwoReceiptsCumulativeGas(t *testing.T) {
	first := Receipt{Status: 1, GasUsed: 21000, CumulativeGasUsed: 21000}
	second := Receipt{Status: 1, GasUsed: 21000, CumulativeGasUsed: 42000}
	if first.CumulativeGasUsed != 21000 || second.CumulativeGasUsed != 42000 {
		t.Fatalf("cumulative = %d %d", first.CumulativeGasUsed, second.CumulativeGasUsed)
	}
	raw := first.Bytes()
	if len(raw) != 24 {
		t.Fatalf("receipt bytes = %d", len(raw))
	}
	if binary.BigEndian.Uint64(raw[0:8]) != 1 ||
		binary.BigEndian.Uint64(raw[8:16]) != 21000 ||
		binary.BigEndian.Uint64(raw[16:24]) != 21000 {
		t.Fatalf("receipt encoding = %x", raw)
	}
	root := ReceiptsRoot([]Receipt{first, second})
	if root == ReceiptsRoot(nil) {
		t.Fatal("two-receipt root matched the empty root")
	}
	flatCumulative := []Receipt{
		first,
		{Status: 1, GasUsed: 21000, CumulativeGasUsed: 21000},
	}
	if ReceiptsRoot(flatCumulative) == root {
		t.Fatal("root ignored cumulative gas")
	}
}
