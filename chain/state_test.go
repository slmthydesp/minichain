package chain

import "testing"

func TestEmptyStateRoot(t *testing.T) {
	if NewState().Root() != emptyTrieRoot() {
		t.Fatalf("empty root = %s", NewState().Root().Hex())
	}
	if emptyTrieRoot().Hex() != emptyTrieRootHex {
		t.Fatalf("empty trie = %s", emptyTrieRoot().Hex())
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

func TestCopyAndBalanceNonce(t *testing.T) {
	addr := Address{19: 3}
	st := NewState()
	st.Put(Account{Address: addr, Balance: 8, Nonce: 2})
	copied := st.Copy()
	copied.Put(Account{Address: addr, Balance: 1, Nonce: 9})
	balance, nonce := st.BalanceNonce(addr)
	if balance != 8 || nonce != 2 {
		t.Fatalf("original balance=%d nonce=%d", balance, nonce)
	}
	copiedBalance, copiedNonce := copied.BalanceNonce(addr)
	if copiedBalance != 1 || copiedNonce != 9 {
		t.Fatalf("copy balance=%d nonce=%d", copiedBalance, copiedNonce)
	}
	missingBalance, missingNonce := st.BalanceNonce(Address{19: 9})
	if missingBalance != 0 || missingNonce != 0 {
		t.Fatalf("missing balance=%d nonce=%d", missingBalance, missingNonce)
	}
}
