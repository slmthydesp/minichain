package chain

import "bytes"

// State 是某一区块执行完后的账户表。coinbase、区块高度和时间只在执行这一拍使用，不写进状态根。
// 代码和存储单独存放，账户结构本身仍然只有地址、余额和 nonce。
// 状态根只从 accounts 里的账户算起：每个账户再带上自己的存储根和代码哈希。
// origin、gasPrice、瞬时存储、本笔交易新建和自毁的地址、最近区块哈希，也只在执行时用，不进状态根。
type State struct {
	accounts map[Address]Account // 会进状态树的账户。余额变成 0 不会自动删掉。
	code     map[Address][]byte  // 合约代码。没有代码的地址不在这张表里。
	// storage 是两层表。外层键是合约地址。
	// 内层键是槽位编号本身：0、1、2 这样的数，写成 32 字节。SLOAD 和 SSTORE 用栈上的这个字直接查表，这里不存 Keccak-256。
	// 内层值是槽里的 32 字节。写成全 0 就从内层表删掉。
	// 算存储根时才对槽位做 Keccak-256。那个哈希只作为存储树的键，不写回这张表。
	storage     map[Address]map[Hash]Hash
	coinbase    Address // 这一拍收取 gas 费用的账户。换块时由调用方重新指定。
	blockNumber uint64  // 这一拍的区块高度。合约用 NUMBER 读取。
	blockTime   uint64  // 这一拍的区块时间。合约用 TIMESTAMP 读取。

	origin      Address                   // 这笔交易的发送方。ORIGIN 读它。调用帧里的 CALLER 不是它。
	gasPrice    uint64                    // 这笔交易的单价。GASPRICE 读它。
	readOnly    bool                      // 为真时这一帧不能写状态。STATICCALL 会把它设上，内层调用也跟着不能写。
	transient   map[Address]map[Hash]Hash // 瞬时存储。TLOAD 和 TSTORE 用。一笔交易结束就丢掉，不进状态根。
	created     map[Address]bool          // 这笔交易里新建的合约。SELFDESTRUCT 只删除这些地址。
	destructed  map[Address]bool          // 这笔交易里执行过 SELFDESTRUCT 的地址。交易成功结束时才真正删。
	blockHashes map[uint64]Hash           // 最近区块的哈希。BLOCKHASH 只回答最近 256 块。
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
	next.blockNumber = s.blockNumber
	next.blockTime = s.blockTime
	next.origin = s.origin
	next.gasPrice = s.gasPrice
	next.readOnly = s.readOnly
	next.transient = copySlots(s.transient)
	next.created = copyAddrSet(s.created)
	next.destructed = copyAddrSet(s.destructed)
	if len(s.blockHashes) > 0 {
		next.blockHashes = make(map[uint64]Hash, len(s.blockHashes))
		for n, sum := range s.blockHashes {
			next.blockHashes[n] = sum
		}
	}
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

// restore 把账户、代码、存储和这一拍的执行上下文换回快照。
// 换回来的是一份新拷贝。之后再改当前状态，不会写进 snap，同一份快照可以再用。
func (s *State) restore(snap *State) {
	*s = *snap.Copy()
}

// SetCoinbase 指定这一拍执行时收取 gas 费用的账户。
func (s *State) SetCoinbase(a Address) {
	s.coinbase = a
}

// SetBlock 指定这一拍的区块高度和时间。合约用 NUMBER 和 TIMESTAMP 读取，这两项不写进状态根。
func (s *State) SetBlock(number, time uint64) {
	s.blockNumber = number
	s.blockTime = time
}

// NoteBlockHash 记下某一高度的区块哈希，供这一拍的 BLOCKHASH 读取。
// 只留最近 256 块。更早的删掉。这项不写进状态根。
func (s *State) NoteBlockHash(height uint64, sum Hash) {
	if s.blockHashes == nil {
		s.blockHashes = make(map[uint64]Hash)
	}
	s.blockHashes[height] = sum
	if height > 256 {
		delete(s.blockHashes, height-257)
	}
}

// beginTx 开始执行一笔交易。ORIGIN 和 GASPRICE 读这里记下的发送方和单价。
// 上一笔留下的瞬时存储、新建和自毁标记清掉。只读标记也清掉。
func (s *State) beginTx(origin Address, gasPrice uint64) {
	s.origin = origin
	s.gasPrice = gasPrice
	s.readOnly = false
	s.transient = nil
	s.created = nil
	s.destructed = nil
}

// endTx 丢掉只在这一笔里有效的执行上下文。账户、代码和存储不动。
func (s *State) endTx() {
	s.readOnly = false
	s.transient = nil
	s.created = nil
	s.destructed = nil
}

// blockHash 返回高度 n 的区块哈希。n 不在 [当前高度-256, 当前高度) 里时是 0。
func (s *State) blockHash(n uint64) Hash {
	if n >= s.blockNumber || s.blockNumber-n > 256 {
		return Hash{}
	}
	return s.blockHashes[n]
}

func (s *State) GetAccount(a Address) (Account, bool) {
	acct, ok := s.accounts[a]
	return acct, ok
}

// GetAccountOrZero 返回账户。不存在时给出余额 0、nonce 0 的账户，地址填好，但不写入状态。
func (s *State) GetAccountOrZero(a Address) Account {
	acct, ok := s.GetAccount(a)
	if !ok {
		return Account{Address: a}
	}
	return acct
}

func (s *State) SetAccount(a Account) {
	s.accounts[a.Address] = a
}

// GetBalanceNonce 返回链上的余额和 nonce。账户不存在时两者都是 0，不因此建立账户。
func (s *State) GetBalanceNonce(a Address) (Uint256, uint64) {
	acct := s.GetAccountOrZero(a)
	return acct.Balance, acct.Nonce
}

// DeleteAccount 只从账户表删掉这一项。代码和存储留在各自的表里，但账户不在表里时，它们不会进入状态根。
func (s *State) DeleteAccount(a Address) {
	delete(s.accounts, a)
}

// GetCode 返回合约代码。没有代码时是 nil。
func (s *State) GetCode(a Address) []byte {
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

// GetStorage 读取一个存储槽。没有写过的槽是 0。
func (s *State) GetStorage(a Address, key Hash) Hash {
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
func (s *State) transfer(from, to Address, value Uint256) error {
	if value.IsZero() || from == to {
		return nil
	}
	src := s.GetAccountOrZero(from)
	if src.Balance.Cmp(value) < 0 {
		return ErrInsufficientBalance
	}
	dst := s.GetAccountOrZero(to)
	sum, overflow := dst.Balance.Add(value)
	if overflow {
		return ErrBalanceOverflow
	}
	next, _ := src.Balance.Sub(value)
	src.Balance = next
	dst.Balance = sum
	s.SetAccount(src)
	s.SetAccount(dst)
	return nil
}

// StateRoot 把账户表收成一个状态根。没有账户时就是空树根。
// 每个账户先做成一对键值，再全部放进同一棵 Merkle Patricia Trie。
// 键是 Keccak-256(地址)，不是地址本身，所以地址的字节顺序不决定它在树上的位置。
// 值是 RLP 列表，顺序固定为 nonce、余额、这个账户的存储根、代码哈希。
// 后两项要先算出来：存储槽再成一棵树，代码做 Keccak-256。没有代码时，是空字节的 Keccak-256。
// accounts 是 map，Go 的 map 没有稳定顺序，插入先后和这次遍历先后都不决定根。
// 树按键的内容分叉：同一批账户，先插谁后插谁，根都相同。
func StateRoot(s *State) Hash {
	if len(s.accounts) == 0 {
		return emptyTrieRoot()
	}
	// keys[i] 和 vals[i] 是同一个账户。先凑齐所有对，再一次性建树。
	// eoa 和合约地址账户信息都存储在 accounts 表里
	keys := make([][]byte, 0, len(s.accounts))
	vals := make([][]byte, 0, len(s.accounts))
	// 这里按 map 遍历，顺序不确定。insertTrie 按 Keccak-256(地址) 落点，不按这次 append 的先后。
	for addr, acct := range s.accounts {
		// 树上的键是地址的 keccak 哈希。拷一份，避免后面改到 keccak 返回的底层数组。
		sum := keccak(addr[:])
		keys = append(keys, append([]byte(nil), sum[:]...))
		// 值里的后两项本身也是哈希，必须先算好再编码。
		storage := s.storageRoot(addr)
		code := codeHash(s.code[addr])
		vals = append(vals, rlpList(
			rlpUint(acct.Nonce),
			acct.Balance.rlp(),
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

// GetTransient 读取瞬时存储。没写过的槽是 0。
func (s *State) GetTransient(a Address, key Hash) Hash {
	slots := s.transient[a]
	if slots == nil {
		return Hash{}
	}
	return slots[key]
}

// SetTransient 写入瞬时存储。写成 0 就删掉这个槽。
func (s *State) SetTransient(a Address, key, val Hash) {
	slots := s.transient[a]
	if slots == nil {
		slots = make(map[Hash]Hash)
		if s.transient == nil {
			s.transient = make(map[Address]map[Hash]Hash)
		}
		s.transient[a] = slots
	}
	if val == (Hash{}) {
		delete(slots, key)
		return
	}
	slots[key] = val
}

// markCreated 记下这个地址是这笔交易新建的。失败的创建会随快照一起丢掉。
func (s *State) markCreated(a Address) {
	if s.created == nil {
		s.created = make(map[Address]bool)
	}
	s.created[a] = true
}

// markDestructed 记下这个地址执行了 SELFDESTRUCT。真正删除要等交易成功结束。
func (s *State) markDestructed(a Address) {
	if s.destructed == nil {
		s.destructed = make(map[Address]bool)
	}
	s.destructed[a] = true
}

// finishSelfDestruct 处理这笔交易里的 SELFDESTRUCT。
// 只有本笔新建的合约才删代码和存储。更早部署的合约只是已经把余额转走，代码还在。
// 删除时余额若又被转入，账户留下余额，nonce 归零，代码和存储清掉。
func (s *State) finishSelfDestruct() {
	for addr := range s.destructed {
		if !s.created[addr] {
			continue
		}
		s.SetCode(addr, nil)
		delete(s.storage, addr)
		acct, ok := s.GetAccount(addr)
		if !ok {
			continue
		}
		if acct.Balance.IsZero() {
			s.DeleteAccount(addr)
			continue
		}
		acct.Nonce = 0
		s.SetAccount(acct)
	}
}

func copySlots(src map[Address]map[Hash]Hash) map[Address]map[Hash]Hash {
	if len(src) == 0 {
		return nil
	}
	out := make(map[Address]map[Hash]Hash, len(src))
	for addr, slots := range src {
		cloned := make(map[Hash]Hash, len(slots))
		for key, val := range slots {
			cloned[key] = val
		}
		out[addr] = cloned
	}
	return out
}

func copyAddrSet(src map[Address]bool) map[Address]bool {
	if len(src) == 0 {
		return nil
	}
	out := make(map[Address]bool, len(src))
	for addr := range src {
		out[addr] = true
	}
	return out
}
