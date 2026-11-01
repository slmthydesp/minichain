// Package chain 是一条只做转账的内存链。
// 区块按哈希保存，每个块带一份状态快照。链头取高度更高的分叉；高度相同则哈希字节更小的胜出。
// 链头往前 FinalityDepth 个块之后，更早的分叉不能再成为链头。
package chain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/slmthydesp/minichain/sig"
)

// Unit 是 1 个币对应的最小单位数量，等于 10^18。余额和转账金额都按最小单位记账。
const Unit uint64 = 1_000_000_000_000_000_000

// GenesisBalance 是创世时演示账户的余额，等于 1 个币。
const GenesisBalance uint64 = Unit

// FinalityDepth 是最终性深度。链头高度不到这个数时，最终确定的只有创世块。
const FinalityDepth uint64 = 12

// Chain 按区块哈希保存块和对应的状态快照。canonical 是当前链头，state 指向链头的快照。
type Chain struct {
	mu        sync.Mutex
	blocks    map[Hash]Block
	states    map[Hash]*State
	canonical Hash
	state     *State
	pool      *Pool
}

// New 创建创世块。高度是 0，父哈希是 32 个零字节，演示账户余额是 GenesisBalance。
func New() *Chain {
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		panic(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		panic(err)
	}
	var addr Address
	copy(addr[:], raw[:])
	st := NewState()
	st.Put(Account{Address: addr, Balance: GenesisBalance, Nonce: 0})
	block := Block{
		Height:           0,
		StateRoot:        st.Root(),
		TransactionsRoot: TransactionsRoot(nil),
		ReceiptsRoot:     ReceiptsRoot(nil),
	}
	sum := block.Hash()
	return &Chain{
		blocks:    map[Hash]Block{sum: block},
		states:    map[Hash]*State{sum: st},
		canonical: sum,
		state:     st,
		pool:      NewPool(),
	}
}

// Head 返回当前选中的链头。
func (c *Chain) Head() Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.blocks[c.canonical]
}

func (c *Chain) Has(h Hash) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.blocks[h]
	return ok
}

func (c *Chain) Block(h Hash) (Block, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	block, ok := c.blocks[h]
	return block, ok
}

func (c *Chain) Account(a Address) (Account, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.Get(a)
}

// NextNonce 是下一笔还能放进本机交易池的 nonce。链头上的值已经被入块交易用过，池里占住的连续号码也会跳过。
func (c *Chain) NextNonce(a Address) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, nonce := c.state.BalanceNonce(a)
	return c.pool.NextNonce(a, nonce)
}

func (c *Chain) Pool() *Pool {
	return c.pool
}

// AddTx 按链头上的余额和 nonce 把交易放进池，并返回恢复出的发送方。
func (c *Chain) AddTx(tx Tx) (Address, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sender, err := tx.Sender()
	if err != nil {
		return Address{}, ErrBadSignature
	}
	balance, nonce := c.state.BalanceNonce(sender)
	if err := c.pool.Add(tx, balance, nonce); err != nil {
		return Address{}, err
	}
	return sender, nil
}

func (c *Chain) DropTx(sender Address, nonce uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pool.Drop(sender, nonce)
}

// BlocksAfter 从链头往回走到指定高度，再按从旧到新的顺序返回这些区块。
func (c *Chain) BlocksAfter(height uint64) []Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	var rev []Block
	hash := c.canonical
	for {
		block, ok := c.blocks[hash]
		if !ok || block.Height <= height {
			break
		}
		rev = append(rev, block)
		if block.Height == 0 {
			break
		}
		hash = block.ParentHash
	}
	out := make([]Block, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out
}

// Mine 打包一个新块。timestamp 必须大于父块时间。池是空的也会出块。
// 同一发送方只按连续 nonce 装入。不同发送方之间，先装下一笔 gasPrice 更高的；单价相同则地址更小的先装。
// 下一笔的 gasLimit 超出区块剩余 gas，或执行失败时，停在这个发送方，换下一个发送方。
// 装完后按高度、哈希和最终性重新选择链头。
func (c *Chain) Mine(coinbase Address, timestamp uint64) (Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	head := c.blocks[c.canonical]
	if timestamp <= head.Time {
		return Block{}, errors.New("bad timestamp")
	}
	pending := c.pool.Pending()
	scratch := c.state.Copy()
	scratch.SetCoinbase(coinbase)
	type lane struct {
		sender Address
		txs    []Tx
		i      int
		nonce  uint64
		open   bool
	}
	lanes := make([]lane, 0)
	for _, tx := range pending {
		sender, err := tx.Sender()
		if err != nil {
			return Block{}, err
		}
		if n := len(lanes); n == 0 || lanes[n-1].sender != sender {
			_, nonce := scratch.BalanceNonce(sender)
			lanes = append(lanes, lane{sender: sender, nonce: nonce, open: true})
		}
		lanes[len(lanes)-1].txs = append(lanes[len(lanes)-1].txs, tx)
	}
	chosen := make([]Tx, 0)
	receipts := make([]Receipt, 0)
	var gasUsed uint64
	remaining := BlockGasLimit
	for {
		best := -1
		for i := range lanes {
			ln := &lanes[i]
			if !ln.open || ln.i >= len(ln.txs) {
				ln.open = false
				continue
			}
			tx := ln.txs[ln.i]
			if tx.Nonce != ln.nonce || tx.GasLimit > remaining {
				ln.open = false
				continue
			}
			if best < 0 {
				best = i
				continue
			}
			cur := lanes[best].txs[lanes[best].i]
			if tx.GasPrice > cur.GasPrice || (tx.GasPrice == cur.GasPrice && bytes.Compare(ln.sender[:], lanes[best].sender[:]) < 0) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		ln := &lanes[best]
		tx := ln.txs[ln.i]
		receipt, err := Apply(scratch, tx)
		if err != nil {
			ln.open = false
			continue
		}
		remaining -= tx.GasLimit
		gasUsed += receipt.GasUsed
		receipt.CumulativeGasUsed = gasUsed
		chosen = append(chosen, tx)
		receipts = append(receipts, receipt)
		ln.i++
		ln.nonce++
	}
	block := Block{
		Height:           head.Height + 1,
		ParentHash:       c.canonical,
		Coinbase:         coinbase,
		GasLimit:         BlockGasLimit,
		GasUsed:          gasUsed,
		Time:             timestamp,
		Txs:              chosen,
		StateRoot:        scratch.Root(),
		TransactionsRoot: TransactionsRoot(chosen),
		ReceiptsRoot:     ReceiptsRoot(receipts),
		Receipts:         receipts,
	}
	sum := block.Hash()
	c.blocks[sum] = block
	c.states[sum] = scratch
	c.adopt(c.best())
	return block, nil
}

// Append 收下别人的区块。已知哈希直接返回。未知父块、高度不连续或时间不晚于父块则拒绝。
// gasLimit 必须是 BlockGasLimit，交易的 gasLimit 加起来也不能超过它，gasUsed 必须等于实际消耗。
// 交易根先与区块体核对。再从父块状态重放交易，状态根或回执根对不上就拒绝，链和状态都不改。
// 不延伸已最终确定区块的分叉仍会保存，但不会成为链头。
func (c *Chain) Append(b Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	sum := b.Hash()
	if _, ok := c.blocks[sum]; ok {
		return nil
	}
	parent, ok := c.blocks[b.ParentHash]
	if !ok {
		return errors.New("unknown parent")
	}
	if b.Height != parent.Height+1 {
		return errors.New("bad height")
	}
	if b.Time <= parent.Time {
		return errors.New("bad timestamp")
	}
	if b.GasLimit != BlockGasLimit {
		return errors.New("bad gas limit")
	}
	if TransactionsRoot(b.Txs) != b.TransactionsRoot {
		return errors.New("bad transactions root")
	}
	scratch := c.states[b.ParentHash].Copy()
	scratch.SetCoinbase(b.Coinbase)
	var gasUsed uint64
	var gasLimitUsed uint64
	receipts := make([]Receipt, 0, len(b.Txs))
	for _, tx := range b.Txs {
		if tx.GasLimit > BlockGasLimit-gasLimitUsed {
			return errors.New("block gas exceeded")
		}
		gasLimitUsed += tx.GasLimit
		receipt, err := Apply(scratch, tx)
		if err != nil {
			return err
		}
		gasUsed += receipt.GasUsed
		receipt.CumulativeGasUsed = gasUsed
		receipts = append(receipts, receipt)
	}
	if gasUsed != b.GasUsed {
		return errors.New("bad gas used")
	}
	if scratch.Root() != b.StateRoot {
		return errors.New("bad state root")
	}
	if ReceiptsRoot(receipts) != b.ReceiptsRoot {
		return errors.New("bad receipts root")
	}
	b.Receipts = receipts
	c.blocks[sum] = b
	c.states[sum] = scratch
	c.adopt(c.best())
	return nil
}

// best 在延伸已最终确定区块的链里选链头：高度更高的胜出；高度相同，哈希字节更小的胜出。
func (c *Chain) best() Hash {
	checkpoint := c.checkpoint()
	var best Hash
	var have bool
	for hash := range c.blocks {
		if !c.extends(hash, checkpoint) {
			continue
		}
		if !have || c.outranks(hash, best) {
			best = hash
			have = true
		}
	}
	return best
}

// checkpoint 是当前链上已经最终确定的区块。链头高度小于 FinalityDepth 时就是创世块。
func (c *Chain) checkpoint() Hash {
	height := uint64(0)
	head := c.blocks[c.canonical]
	if head.Height >= FinalityDepth {
		height = head.Height - FinalityDepth
	}
	return c.blockAt(c.canonical, height)
}

func (c *Chain) blockAt(tip Hash, height uint64) Hash {
	hash := tip
	for {
		block := c.blocks[hash]
		if block.Height <= height {
			return hash
		}
		hash = block.ParentHash
	}
}

func (c *Chain) extends(tip, ancestor Hash) bool {
	hash := tip
	for {
		if hash == ancestor {
			return true
		}
		block, ok := c.blocks[hash]
		if !ok || block.Height == 0 {
			return false
		}
		hash = block.ParentHash
	}
}

func (c *Chain) outranks(hash, best Hash) bool {
	block := c.blocks[hash]
	cur := c.blocks[best]
	return block.Height > cur.Height || (block.Height == cur.Height && bytes.Compare(hash[:], best[:]) < 0)
}

// adopt 把链头切到 next。先找到与旧链头的共同祖先，丢掉新分叉里已经入块的交易，
// 再把旧分叉里还没在新链头花掉的交易放回池。状态直接换成新链头的快照。
func (c *Chain) adopt(next Hash) {
	if next == c.canonical {
		return
	}
	prev := c.canonical
	ancestor := c.commonAncestor(prev, next)
	fresh := c.txsBetween(ancestor, next)
	stale := c.txsBetween(ancestor, prev)
	c.canonical = next
	c.state = c.states[next]
	spent := make(map[string]struct{}, len(fresh))
	for _, tx := range fresh {
		sender, err := tx.Sender()
		if err != nil {
			continue
		}
		spent[nonceKey(sender, tx.Nonce)] = struct{}{}
		c.pool.Drop(sender, tx.Nonce)
	}
	for _, tx := range stale {
		sender, err := tx.Sender()
		if err != nil {
			continue
		}
		if _, ok := spent[nonceKey(sender, tx.Nonce)]; ok {
			continue
		}
		balance, nonce := c.state.BalanceNonce(sender)
		_ = c.pool.Add(tx, balance, nonce)
	}
}

// commonAncestor 从两条分叉往回走，返回最近的共同祖先。
func (c *Chain) commonAncestor(a, b Hash) Hash {
	seen := make(map[Hash]bool)
	for {
		seen[a] = true
		block, ok := c.blocks[a]
		if !ok || block.Height == 0 {
			break
		}
		a = block.ParentHash
	}
	for {
		if seen[b] {
			return b
		}
		block, ok := c.blocks[b]
		if !ok || block.Height == 0 {
			return b
		}
		b = block.ParentHash
	}
}

// txsBetween 返回 (ancestor, tip] 上的交易，顺序是从祖先的下一块到 tip。
func (c *Chain) txsBetween(ancestor, tip Hash) []Tx {
	var groups [][]Tx
	for tip != ancestor {
		block, ok := c.blocks[tip]
		if !ok {
			break
		}
		groups = append(groups, block.Txs)
		if block.Height == 0 {
			break
		}
		tip = block.ParentHash
	}
	out := make([]Tx, 0)
	for i := len(groups) - 1; i >= 0; i-- {
		out = append(out, groups[i]...)
	}
	return out
}

func nonceKey(sender Address, nonce uint64) string {
	var buf [28]byte
	copy(buf[:20], sender[:])
	binary.BigEndian.PutUint64(buf[20:], nonce)
	return string(buf[:])
}
