package chain

import "testing"

func TestEmptyStateRoot(t *testing.T) {
	want := keccak(nil)
	if NewState().Root() != want {
		t.Fatalf("empty root = %s", NewState().Root().Hex())
	}
	parsed, err := ParseHash("0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470")
	if err != nil {
		t.Fatal(err)
	}
	if want != parsed {
		t.Fatalf("keccak(nil) = %s", want.Hex())
	}
}

func TestStateRootChangesOnPutModifyDelete(t *testing.T) {
	a := Account{Address: Address{19: 1}, Balance: 5, Nonce: 1}
	b := Account{Address: Address{19: 2}, Balance: 0, Nonce: 0}
	left := NewState()
	left.Put(a)
	left.Put(b)
	right := NewState()
	right.Put(b)
	right.Put(a)
	if left.Root() != right.Root() {
		t.Fatal("insertion order changed the root")
	}
	empty := NewState().Root()
	if left.Root() == empty {
		t.Fatal("insert did not change the root")
	}
	modified := left.Copy()
	a.Balance = 6
	modified.Put(a)
	if modified.Root() == left.Root() {
		t.Fatal("modify did not change the root")
	}
	deleted := left.Copy()
	deleted.Delete(b.Address)
	if deleted.Root() == left.Root() {
		t.Fatal("delete did not change the root")
	}
	again := deleted.Root()
	deleted.Delete(Address{19: 9})
	if deleted.Root() != again {
		t.Fatal("deleting a missing account changed the root")
	}
	zero := NewState()
	zero.Put(b)
	if zero.Root() == empty {
		t.Fatal("zero-balance account was omitted")
	}
}
