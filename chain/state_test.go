package chain

import "testing"

// 空状态的根等于空树根，空树根的十六进制也等于约定的常量。
func TestEmptyStateRoot(t *testing.T) {
	// 没有任何账户时，状态根就是空树根。
	if StateRoot(NewState()) != emptyTrieRoot() {
		t.Fatalf("empty root = %s", StateRoot(NewState()).Hex())
	}
	// 空树根的十六进制要和写死的常量一致。
	if emptyTrieRoot().Hex() != emptyTrieRootHex {
		t.Fatalf("empty trie = %s", emptyTrieRoot().Hex())
	}
}

// 两个账户互换插入顺序，状态根相同。
// 插入之后根不再是空树根。改余额、删掉已有账户都会改变根。
// 删除不存在的账户不改变根。余额为 0 的账户也写进树，根不是空树根。
func TestStateRootChangesOnPutModifyDelete(t *testing.T) {
	a := Account{Address: Address{19: 1}, Balance: 5, Nonce: 1}
	b := Account{Address: Address{19: 2}, Balance: 0, Nonce: 0}
	left := NewState()
	left.SetAccount(a)
	left.SetAccount(b)
	right := NewState()
	right.SetAccount(b)
	right.SetAccount(a)
	// 先放 a 再放 b，和先放 b 再放 a，根相同。
	if StateRoot(left) != StateRoot(right) {
		t.Fatal("insertion order changed the root")
	}
	empty := StateRoot(NewState())
	// 放进账户之后，根不再是空树根。
	if StateRoot(left) == empty {
		t.Fatal("insert did not change the root")
	}
	modified := left.Copy()
	a.Balance = 6
	modified.SetAccount(a)
	// 只改余额，根也要变。副本上的修改不动原来那份。
	if StateRoot(modified) == StateRoot(left) {
		t.Fatal("modify did not change the root")
	}
	deleted := left.Copy()
	deleted.DeleteAccount(b.Address)
	// 删掉已经在树上的账户，根要变。
	if StateRoot(deleted) == StateRoot(left) {
		t.Fatal("delete did not change the root")
	}
	again := StateRoot(deleted)
	deleted.DeleteAccount(Address{19: 9})
	// 要删的账户本来就不在，根保持不变。
	if StateRoot(deleted) != again {
		t.Fatal("deleting a missing account changed the root")
	}
	zero := NewState()
	zero.SetAccount(b)
	// b 的余额是 0，仍然算一个账户，不能当成空状态。
	if StateRoot(zero) == empty {
		t.Fatal("zero-balance account was omitted")
	}
}

// 副本上改余额和 nonce，原状态仍是 8 和 2。副本读到的是改过的 1 和 9。
// 没有的账户读出来是余额 0、nonce 0。
func TestCopyAndBalanceNonce(t *testing.T) {
	addr := Address{19: 3}
	st := NewState()
	st.SetAccount(Account{Address: addr, Balance: 8, Nonce: 2})
	copied := st.Copy()
	copied.SetAccount(Account{Address: addr, Balance: 1, Nonce: 9})
	// 改副本，原状态的余额和 nonce 不动。
	balance, nonce := st.GetBalanceNonce(addr)
	if balance != 8 || nonce != 2 {
		t.Fatalf("original balance=%d nonce=%d", balance, nonce)
	}
	// 副本读到的是刚写进去的 1 和 9。
	copiedBalance, copiedNonce := copied.GetBalanceNonce(addr)
	if copiedBalance != 1 || copiedNonce != 9 {
		t.Fatalf("copy balance=%d nonce=%d", copiedBalance, copiedNonce)
	}
	// 没建过的地址按余额 0、nonce 0 读。
	missingBalance, missingNonce := st.GetBalanceNonce(Address{19: 9})
	if missingBalance != 0 || missingNonce != 0 {
		t.Fatalf("missing balance=%d nonce=%d", missingBalance, missingNonce)
	}
}

// 快照和当前状态各是一份数据。restore 用快照盖当前状态时，会再拷一次，快照自己不被改掉，所以同一份快照可以恢复多次
// 先改当前状态，再换回快照，然后在当前状态上再改一次。快照仍是最初的 8 和 2。
// 用同一份快照再恢复一次，当前状态回到最初。
func TestRestoreLeavesSnapshotReusable(t *testing.T) {
	addr := Address{19: 3}
	st := NewState()
	st.SetAccount(Account{Address: addr, Balance: 8, Nonce: 2})
	snap := st.Copy()
	st.SetAccount(Account{Address: addr, Balance: 1, Nonce: 9})
	// 改过之后换回快照。
	st.restore(snap)
	st.SetAccount(Account{Address: addr, Balance: 4, Nonce: 3})
	// 恢复之后又改了当前状态。快照自己还是最初的 8 和 2。
	balance, nonce := snap.GetBalanceNonce(addr)
	if balance != 8 || nonce != 2 {
		t.Fatalf("snapshot balance=%d nonce=%d", balance, nonce)
	}
	st.restore(snap)
	// 同一份快照可以再用一次，当前状态回到 8 和 2。
	balance, nonce = st.GetBalanceNonce(addr)
	if balance != 8 || nonce != 2 {
		t.Fatalf("restored balance=%d nonce=%d", balance, nonce)
	}
}

// 副本上改存储槽、把代码写成空，原状态的槽仍是 9，代码仍是那 1 字节。
// 空代码会从代码表里删掉，副本上读出来是空的。
func TestCopyStorageAndEmptyCode(t *testing.T) {
	addr := Address{19: 4}
	key := hashFromUint64(1)
	st := NewState()
	st.SetCode(addr, []byte{0x00})
	st.SetStorage(addr, key, hashFromUint64(9))
	copied := st.Copy()
	copied.SetStorage(addr, key, hashFromUint64(3))
	copied.SetCode(addr, nil)
	// 改副本的槽，原状态还是 9。
	if st.GetStorage(addr, key) != hashFromUint64(9) {
		t.Fatalf("original storage = %x", st.GetStorage(addr, key))
	}
	// 副本上清掉代码，原状态的代码还在。
	if len(st.GetCode(addr)) != 1 {
		t.Fatalf("original code = %x", st.GetCode(addr))
	}
	// 写成空就从表里删掉，副本上不再留这段代码。
	if len(copied.GetCode(addr)) != 0 {
		t.Fatal("empty code was kept")
	}
}

// 余额只有 1，转出 2 返回余额不足。
// 槽里只剩全 0 的值时，这个值不进存储树，存储根是空树根。
func TestTransferInsufficientAndZeroStorageRoot(t *testing.T) {
	st := NewState()
	from := Address{19: 1}
	to := Address{19: 2}
	st.SetAccount(Account{Address: from, Balance: 1})
	// 转出的数量大于余额。
	if err := st.transfer(from, to, 2); err != ErrInsufficientBalance {
		t.Fatalf("err = %v", err)
	}
	// SetStorage 会直接丢掉全 0 的槽，这里绕过它，把全 0 的槽留在表里。
	st.storage[from] = map[Hash]Hash{hashFromUint64(1): {}}
	// 算存储根时跳过全 0 的槽。一个槽都不进树，根就是空树根。
	if st.storageRoot(from) != emptyTrieRoot() {
		t.Fatal("zero slots should be an empty storage root")
	}
}
