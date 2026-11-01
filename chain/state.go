package chain

import "bytes"

// State 是某一区块执行完后的账户表。coinbase 只在执行这一拍使用，不写进状态根。
// 代码和存储单独存放，账户结构本身仍然只有地址、余额和 nonce。
// 状态根只从 accounts 里的账户算起：每个账户再带上自己的存储根和代码哈希。
type State struct {
	accounts map[Address]Account // 会进状态树的账户。余额变成 0 不会自动删掉。
	code     map[Address][]byte  // 合约代码。没有代码的地址不在这张表里。
	// storage 是两层表。外层键是合约地址，内层键是这个合约的 32 字节槽位，内层值是槽里的 32 字节。
	// 写成全 0 的槽会从内层表删掉。算存储根时，槽位再做一次 Keccak-256，才作为存储树的键。
	storage  map[Address]map[Hash]Hash
	coinbase Address // 这一拍收取 gas 费用的账户。换块时由调用方重新指定。
}

func NewState() *State {
	return &State{
		accounts: make(map[Address]Account),
		code:     make(map[Address][]byte),
		storage:  make(map[Address]map[Hash]Hash),
	}
}

// Copy 复制一份账户表。分叉上的每个区块各自保留一份，回滚时直接换回旧快照。
func (s *State) Copy() *State {
	next := NewState()
	next.coinbase = s.coinbase
	for k, v := range s.accounts {
		next.accounts[k] = v
	}
	for addr, code := range s.code {
		next.code[addr] = bytes.Clone(code)
	}
	for addr, slots := range s.storage {
		cloned := make(map[Hash]Hash, len(slots))
		for key, val := range slots {
			cloned[key] = val
		}
		next.storage[addr] = cloned
	}
	return next
}

// restore 把四份引用换回 Copy 得到的快照。快照当时已经分开拷过，所以这里不再复制。
// 执行失败时用它丢掉这次改过的账户、代码和存储。
func (s *State) restore(snap *State) {
	s.accounts = snap.accounts
	s.code = snap.code
	s.storage = snap.storage
	s.coinbase = snap.coinbase
}

// SetCoinbase 指定这一拍执行时收取 gas 费用的账户。
func (s *State) SetCoinbase(a Address) {
	s.coinbase = a
}

func (s *State) Get(a Address) (Account, bool) {
	acct, ok := s.accounts[a]
	return acct, ok
}

func (s *State) Put(a Account) {
	s.accounts[a.Address] = a
}

// BalanceNonce 返回链上的余额和 nonce。账户不存在时两者都是 0，不因此建立账户。
func (s *State) BalanceNonce(a Address) (uint64, uint64) {
	acct, ok := s.accounts[a]
	if !ok {
		return 0, 0
	}
	return acct.Balance, acct.Nonce
}

// Delete 只从账户表删掉这一项。代码和存储留在各自的表里，但账户不在表里时，它们不会进入状态根。
func (s *State) Delete(a Address) {
	delete(s.accounts, a)
}

// Code 返回合约代码。没有代码时是 nil。
func (s *State) Code(a Address) []byte {
	return s.code[a]
}

// SetCode 写入合约代码。空代码等同于没有代码。
func (s *State) SetCode(a Address, code []byte) {
	if len(code) == 0 {
		delete(s.code, a)
		return
	}
	s.code[a] = bytes.Clone(code)
}

// Storage 读取一个存储槽。没有写过的槽是 0。
func (s *State) Storage(a Address, key Hash) Hash {
	slots := s.storage[a]
	if slots == nil {
		return Hash{}
	}
	return slots[key]
}

// SetStorage 写入一个存储槽。写成 0 就删掉这个槽。
func (s *State) SetStorage(a Address, key, val Hash) {
	slots := s.storage[a]
	if slots == nil {
		slots = make(map[Hash]Hash)
		s.storage[a] = slots
	}
	if val == (Hash{}) {
		delete(slots, key)
		return
	}
	slots[key] = val
}

// transfer 把 value 从 from 转到 to。发给自己时余额不变。余额不够或收款方溢出时状态不动。
func (s *State) transfer(from, to Address, value uint64) error {
	if value == 0 || from == to {
		return nil
	}
	src, ok := s.Get(from)
	if !ok {
		src = Account{Address: from}
	}
	if src.Balance < value {
		return ErrInsufficientBalance
	}
	dst, ok := s.Get(to)
	if !ok {
		dst = Account{Address: to}
	}
	if dst.Balance > ^uint64(0)-value {
		return ErrBalanceOverflow
	}
	src.Balance -= value
	dst.Balance += value
	s.Put(src)
	s.Put(dst)
	return nil
}

// Root 把账户表收成一个状态根。没有账户时就是空树根。
// 每个账户先做成一对键值，再全部放进同一棵 Merkle Patricia Trie。
// 键是 Keccak-256(地址)，不是地址本身，所以地址的字节顺序不决定它在树上的位置。
// 值是 RLP 列表，顺序固定为 nonce、余额、这个账户的存储根、代码哈希。
// 后两项要先算出来：存储槽再成一棵树，代码做 Keccak-256。没有代码时，是空字节的 Keccak-256。
// accounts 是 map，遍历顺序每次可能不同。树按键的内容分叉，不按插入顺序，所以根相同。
func (s *State) Root() Hash {
	if len(s.accounts) == 0 {
		return emptyTrieRoot()
	}
	// keys[i] 和 vals[i] 是同一个账户。先凑齐所有对，再一次性建树。
	keys := make([][]byte, 0, len(s.accounts))
	vals := make([][]byte, 0, len(s.accounts))
	for addr, acct := range s.accounts {
		// 树上的键是地址的 keccak 哈希。拷一份，避免后面改到 keccak 返回的底层数组。
		sum := keccak(addr[:])
		keys = append(keys, append([]byte(nil), sum[:]...))
		// 值里的后两项本身也是哈希，必须先算好再编码。
		storage := s.storageRoot(addr)
		code := codeHash(s.code[addr])
		vals = append(vals, rlpList(
			rlpUint(acct.Nonce),
			rlpUint(acct.Balance),
			rlpString(storage[:]),
			rlpString(code[:]),
		))
	}
	return trieRoot(keys, vals)
}

// storageRoot 是这个账户自己的存储树。键是槽位的 Keccak-256，值为零的槽不入树。
func (s *State) storageRoot(addr Address) Hash {
	slots := s.storage[addr]
	if len(slots) == 0 {
		return emptyTrieRoot()
	}
	keys := make([][]byte, 0, len(slots))
	vals := make([][]byte, 0, len(slots))
	for slot, val := range slots {
		if val == (Hash{}) {
			continue
		}
		sum := keccak(slot[:])
		keys = append(keys, append([]byte(nil), sum[:]...))
		raw := val
		vals = append(vals, rlpString(raw[:]))
	}
	if len(keys) == 0 {
		return emptyTrieRoot()
	}
	return trieRoot(keys, vals)
}

// codeHash 没有代码时是空字节的 Keccak-256。
func codeHash(code []byte) Hash {
	if len(code) == 0 {
		return keccak(nil)
	}
	return keccak(code)
}
