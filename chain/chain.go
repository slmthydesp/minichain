// Package chain 是一条内存里的链。账户可以转账，也可以带代码，由虚拟机执行。
// 区块按哈希保存，分叉也留着。每个块带一份执行完的状态。
// 链头先比高度，更高的胜出；一样高则哈希字节更小的胜出。
// 从链头往回数 FinalityDepth 块，那一块已经最终确定。从它前面分出去的叉还能保存，但不能再当链头。
package chain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/slmthydesp/minichain/sig"
)

// Unit 是 1 个币对应的最小单位数量，等于 10^18。余额和转账金额都按最小单位记账，宽度是 256 位。
var Unit = U256(1_000_000_000_000_000_000)

// GenesisBalance 是创世时演示账户的余额，等于 1 个币。
var GenesisBalance = Unit

// FinalityDepth 是最终性深度。链头高度是 H，并且 H 不小于这个数时，高度 H-FinalityDepth 的块已经最终确定。
// 链还更短时，最终确定的只有创世块。
const FinalityDepth uint64 = 12

// Chain 按区块哈希保存块和对应的状态快照。canonical 是当前链头，state 指向链头的快照。
type Chain struct {
	mu        sync.Mutex
	blocks    map[Hash]Block    // 收下的块，分叉也留在这里。键是区块哈希。
	states    map[Hash]*State   // 每个块执行完的状态。键和 blocks 相同。
	canonical Hash              // 当前链头的哈希。它一直是目前的最优块。
	state     *State            // 链头的快照，就是 states[canonical]。
	pool      *Pool             // 还没入块的交易。换链头时按新状态退回或丢掉。
	eligible  map[Hash]struct{} // 从当前最终确定区块延伸出来的块，含那一块。选链头只看这里。
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
	// 创世状态里只有演示账户。coinbase、高度和时间留到后面出块时再写。
	st.SetAccount(Account{Address: addr, Balance: GenesisBalance, Nonce: 0})
	block := Block{
		Height:           0,
		StateRoot:        StateRoot(st),
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
		eligible:  map[Hash]struct{}{sum: {}},
	}
}

// Head 返回当前选中的链头。
func (c *Chain) Head() Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.blocks[c.canonical]
}

// HasBlock 报告这个哈希的区块是否已经收下。只要知道有没有，不必把整块拷出来。
func (c *Chain) HasBlock(h Hash) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.blocks[h]
	return ok
}

// BlockByHash 按哈希取出区块。第二个返回值是假时，这个哈希还没收下。
func (c *Chain) BlockByHash(h Hash) (Block, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	block, ok := c.blocks[h]
	return block, ok
}

// Account 读链头状态下的账户。还没被选成链头的分叉，从这里看不到。
func (c *Chain) Account(a Address) (Account, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.GetAccount(a)
}

// NextNonce 是这个地址下一笔可以放进本机交易池的号码。
// 先取链头账户上的 nonce，再跳过池里从这里连续占住的号码。中间有空号时，返回的是那个空号。
func (c *Chain) NextNonce(a Address) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, nonce := c.state.GetBalanceNonce(a)
	return c.pool.NextNonce(a, nonce)
}

// Pool 返回这条链的交易池。
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
	balance, nonce := c.state.GetBalanceNonce(sender)
	if err := c.pool.AddTx(tx, balance, nonce); err != nil {
		return Address{}, err
	}
	return sender, nil
}

// DropTx 从池里丢掉这一发送方的这个 nonce。链上的账户状态不动。
func (c *Chain) DropTx(sender Address, nonce uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pool.DropTx(sender, nonce)
}

// BlocksAfter 返回当前链头这条链上、高度大于 height 的块，从旧到新。
// 侧链上的块不在结果里。height 那一块本身也不包括。
func (c *Chain) BlocksAfter(height uint64) []Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	var rev []Block
	hash := c.canonical
	// 从链头沿父哈希往回走，先碰到的是较新的块。高度已经不大于 height 就停。
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
	// 倒过来，调用方看到的是从旧到新。
	out := make([]Block, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out
}

// Mine 在当前链头后面接出一个新块，并返回这块。整段持有链锁。
//
//  1. 读出链头。timestamp 必须大于父块时间，否则返回 bad timestamp，链和交易池都不改。
//  2. 取出池里待打包的交易。池是空的也继续，最后出一块没有交易的块。
//  3. 复制链头状态。coinbase、新高度和新时间只写在这份副本上。副本写坏了不会碰链头。
//  4. 按发送方分成若干队。池里已经按地址、再按 nonce 排好，地址一变就新开一队。
//     每队的起始 nonce 读副本上的账户，是这位下一笔必须使用的号码。签名恢复失败则整次出块返回错误。
//  5. 反复挑选下一笔，直到没有队还能装。每一轮只看各队还开着的下一笔：
//     nonce 接不上，或 gasLimit 大于本块剩余容量，就关掉这一队，后面的交易留在池里。
//     还开着的队里，gasPrice 更高的先装；单价相同则地址字节更小的先装。
//     选中后在副本上 Apply。返回错误只关掉这位，本块里已经装上的交易保留，接着看别人。
//     回退和停机仍有回执，照样入块。容量按声明的 gasLimit 扣，gasUsed 累加回执里实际烧掉的。
//     装完后这一队的下标和期望 nonce 都加 1，下一轮才能接着装他的后一笔。
//  6. 组装新块。高度是父块加 1，父哈希是开始时的链头。状态根、交易根和回执根按副本和本块内容现算。
//  7. 按区块哈希保存块和执行完的状态。链头一直是目前的最优块，只把新块和它比。
//     新块的父块就是链头，高度加 1，所以会成为链头。
//     换链头时，本块里的交易已经入块，从池里删掉。返回的就是刚挖出的这块。
func (c *Chain) Mine(coinbase Address, timestamp uint64) (Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	head := c.blocks[c.canonical]
	// 新块时间必须晚于父块。等于或更早都接不上去。
	if timestamp <= head.Time {
		return Block{}, errors.New("bad timestamp")
	}
	// PendingTxs 已按地址、再按 nonce 排好，下面按这个顺序拆成每位发送方一队。
	pending := c.pool.PendingTxs()
	// 在链头状态的副本上打包。副本写坏了不动链头，装完才按新块哈希存进去。
	scratch := c.state.Copy()
	scratch.SetCoinbase(coinbase)
	scratch.SetBlock(head.Height+1, timestamp)
	scratch.NoteBlockHash(head.Height, head.Hash())
	// 一位发送方在本块里排成一队。PendingTxs 已按地址、再按 nonce 排好，地址一变就换下一位。
	type lane struct {
		sender Address // 这一队的发送方。单价相同时用它和别的队比地址。
		txs    []Tx    // 这位在池里的交易，按 nonce 从小到大。
		i      int     // 下一笔还没装的交易在 txs 里的下标。装进一块就加 1。
		nonce  uint64  // 下一笔必须等于的号码。从账户 nonce 起，装进一块就加 1。
		open   bool    // 为假表示这位本块不再装：号码接不上、gas 装不下，或 Apply 返回了错误。
	}
	lanes := make([]lane, 0)
	// pending 数组先按照 tx 的 sender 分组，再按 nonce 排序
	for _, tx := range pending {
		sender, err := tx.Sender()
		if err != nil {
			return Block{}, err
		}
		// 地址变了就新开一队。号码从副本上的账户读出，是这位下一笔必须使用的 nonce。
		if n := len(lanes); n == 0 || lanes[n-1].sender != sender {
			_, nonce := scratch.GetBalanceNonce(sender)
			lanes = append(lanes, lane{sender: sender, nonce: nonce, open: true})
		}
		lanes[len(lanes)-1].txs = append(lanes[len(lanes)-1].txs, tx)
	}
	// chosen 和 receipts 按下标对齐：第 i 笔交易对应第 i 张回执。
	// gasUsed 累加实际烧掉的 gas。remaining 是本块还剩的容量，按交易声明的 gasLimit 扣。
	chosen := make([]Tx, 0)
	receipts := make([]Receipt, 0)
	var gasUsed uint64
	remaining := BlockGasLimit
	// 每一轮只看还开着的发送方各自的下一笔，选出这一轮要装的那一笔。
	for {
		// best 是这一轮选中的队。-1 表示还没有候选人。
		best := -1
		for i := range lanes {
			ln := &lanes[i]
			// 这一队已经关了，或者名下的交易都看完了，就退出后面的轮次。
			if !ln.open || ln.i >= len(ln.txs) {
				ln.open = false
				continue
			}
			tx := ln.txs[ln.i]
			// 号码对不上，说明下一笔接不上账户 nonce，这位后面的交易留在池里。
			// gasLimit 大于本块还剩的 gas，这位本块也不再装。
			if tx.Nonce != ln.nonce || tx.GasLimit > remaining {
				ln.open = false
				continue
			}
			// 这一轮还没有候选人，当前这笔先占住，不用和自己比。
			if best < 0 {
				best = i
				continue
			}
			// 单价更高的赢。单价相同，发送方地址字节更小的赢。
			cur := lanes[best].txs[lanes[best].i]
			if tx.GasPrice > cur.GasPrice || (tx.GasPrice == cur.GasPrice && bytes.Compare(ln.sender[:], lanes[best].sender[:]) < 0) {
				best = i
			}
		}
		// 没有任何发送方还能装，本块到此结束。池是空的也会走到这里，照样出空块。
		if best < 0 {
			break
		}
		ln := &lanes[best]
		tx := ln.txs[ln.i]
		receipt, err := Apply(scratch, tx)
		// Apply 返回错误时，这笔不能入块，只关掉这位。回退和停机仍返回回执，照样装进来。
		// 本块里已经装成功的交易留着，接着看别的发送方。
		if err != nil {
			ln.open = false
			continue
		}
		// 占区块容量按交易声明的 gasLimit。gasUsed 加的是回执里实际烧掉的。
		remaining -= tx.GasLimit
		gasUsed += receipt.GasUsed
		receipt.CumulativeGasUsed = gasUsed
		chosen = append(chosen, tx)
		receipts = append(receipts, receipt)
		// 这笔装进来了。下一次看这位时，必须是 nonce 加 1 的下一笔。
		ln.i++
		ln.nonce++
	}
	// 父哈希是打包开始时的链头。三棵树根按副本上的状态、装进本块的交易和回执现算。
	block := Block{
		Height:           head.Height + 1,
		ParentHash:       c.canonical,
		Coinbase:         coinbase,
		GasLimit:         BlockGasLimit,
		GasUsed:          gasUsed,
		Time:             timestamp,
		Txs:              chosen,
		StateRoot:        StateRoot(scratch),
		TransactionsRoot: TransactionsRoot(chosen),
		ReceiptsRoot:     ReceiptsRoot(receipts),
		Receipts:         receipts,
	}
	sum := block.Hash()
	c.blocks[sum] = block
	c.states[sum] = scratch
	// 父块就是当前链头，prefer 会把链头换成这块。
	c.prefer(sum)
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
	// 同一份区块再来一次，直接当成功。同步时两边可能重复送到。
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

	// 1. 检查交易根
	// 2. 重放交易
	// 3. 检查状态根
	// 4. 检查回执根
	if TransactionsRoot(b.Txs) != b.TransactionsRoot {
		return errors.New("bad transactions root")
	}
	// 从父块状态重放，不碰当前链头。任一步对不上就在写入前返回，已有的块和状态都不改。
	scratch := c.states[b.ParentHash].Copy()
	scratch.SetCoinbase(b.Coinbase)
	scratch.SetBlock(b.Height, b.Time)
	scratch.NoteBlockHash(parent.Height, parent.Hash())
	var gasUsed uint64
	var gasLimitUsed uint64
	receipts := make([]Receipt, 0, len(b.Txs))
	for _, tx := range b.Txs {
		// 每笔先占掉自己声明的 gasLimit。这些上限加起来超过区块 gas 上限，就拒绝整块。
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
	// 重放得到的实际消耗、状态根、回执根，必须和区块头里写的一致。
	if gasUsed != b.GasUsed {
		return errors.New("bad gas used")
	}
	if StateRoot(scratch) != b.StateRoot {
		return errors.New("bad state root")
	}
	if ReceiptsRoot(receipts) != b.ReceiptsRoot {
		return errors.New("bad receipts root")
	}
	b.Receipts = receipts
	c.blocks[sum] = b
	c.states[sum] = scratch
	// 对得上才保存。prefer 只拿这块和当前链头比。赢不了也留着，后面的子块还能接上来。
	c.prefer(sum)
	return nil
}

// prefer 在新块写入 blocks 之后调用。链头一直是目前的最优块，所以只和这块比，不再扫描全部区块。
// 父块不在 eligible 里，说明它从已最终确定的区块前面分出去，赢不了链头。
// 父块在里面就记下这块。比链头更高，或同高且哈希字节更小时，才换成链头。
func (c *Chain) prefer(hash Hash) {
	block := c.blocks[hash]
	if _, ok := c.eligible[block.ParentHash]; !ok {
		return
	}
	// 先记入候选。这块现在赢不了也留着，它的子块以后还能接着比。
	c.eligible[hash] = struct{}{}
	// 比链头更高，或同高且哈希字节更小，才换成链头。
	if c.outranks(hash, c.canonical) {
		c.adopt(hash)
	}
}

// checkpoint 是当前链头上已经最终确定的那一块。
// 链头高度不小于 FinalityDepth 时，取链头高度再减去 FinalityDepth。链更短时就是创世块。
func (c *Chain) checkpoint() Hash {
	// 例如链头高度 15、深度 12，最终确定的是高度 3。从高度 3 前面分出的叉不能再当链头。
	height := uint64(0)
	head := c.blocks[c.canonical]
	if head.Height >= FinalityDepth {
		height = head.Height - FinalityDepth
	}
	return c.blockAtHeight(c.canonical, height)
}

// blockAtHeight 从 tip 沿父哈希往回走，停在高度等于 height 的那一块。
// 高度每一步减 1，所以不会停到更早的块。
func (c *Chain) blockAtHeight(tip Hash, height uint64) Hash {
	hash := tip
	for {
		block := c.blocks[hash]
		if block.Height <= height {
			return hash
		}
		hash = block.ParentHash
	}
}

// extends 报告 tip 是不是 ancestor 自己，或者 ancestor 的后代。
// 沿父哈希走到创世还没遇见 ancestor，就是从更早处分出去的另一条叉。
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

// outranks 比较两个都延伸 checkpoint 的块。高度更大的赢；一样高时，哈希字节更小的赢。
func (c *Chain) outranks(hash, best Hash) bool {
	block := c.blocks[hash]
	cur := c.blocks[best]
	return block.Height > cur.Height || (block.Height == cur.Height && bytes.Compare(hash[:], best[:]) < 0)
}

// adopt 把链头换到 next。next 就是当前链头时什么都不做。
// 共同祖先之后，新叉上的交易从池里删掉。旧叉上同一发送方、同一 nonce 还没在新叉入块的，按新状态试着放回。
// 链头状态换成 next 的快照，两条叉的余额不合并。
func (c *Chain) adopt(next Hash) {
	if next == c.canonical {
		return
	}
	prev := c.canonical
	ancestor := c.commonAncestor(prev, next)
	// fresh、stale 都不含共同祖先自己。
	fresh := c.txsBetween(ancestor, next)
	stale := c.txsBetween(ancestor, prev)
	// 先切状态。后面放回旧交易时，余额和 nonce 以新链头为准。
	c.canonical = next
	c.state = c.states[next]
	spent := make(map[string]struct{}, len(fresh))
	for _, tx := range fresh {
		sender, err := tx.Sender()
		if err != nil {
			continue
		}
		spent[nonceKey(sender, tx.Nonce)] = struct{}{}
		c.pool.DropTx(sender, tx.Nonce)
	}
	for _, tx := range stale {
		sender, err := tx.Sender()
		if err != nil {
			continue
		}
		// 新叉已经用过这个号码，旧叉上的这一笔不再进池。
		if _, ok := spent[nonceKey(sender, tx.Nonce)]; ok {
			continue
		}
		// 对不上新链头的余额或 nonce 时 AddTx 拒绝，交易留在被放弃的块里。
		balance, nonce := c.state.GetBalanceNonce(sender)
		_ = c.pool.AddTx(tx, balance, nonce)
	}
	// 最终确定点前移后，从它前面分出去的块不再可能当链头，从 eligible 里拿掉。
	c.keepEligible()
}

// keepEligible 只留下当前最终确定区块和它的后代。eligible 因此只覆盖最后这十几块，而不是整条链。
func (c *Chain) keepEligible() {
	checkpoint := c.checkpoint()
	next := make(map[Hash]struct{}, len(c.eligible))
	next[checkpoint] = struct{}{}
	for hash := range c.eligible {
		if hash != checkpoint && c.extends(hash, checkpoint) {
			next[hash] = struct{}{}
		}
	}
	c.eligible = next
}

// commonAncestor 返回两条叉最近的共同祖先。
func (c *Chain) commonAncestor(a, b Hash) Hash {
	seen := make(map[Hash]bool)
	// 先把 a 到创世的哈希记下来，再从 b 沿父哈希往回走。第一次遇到记下来的哈希，就是最近的共同祖先。
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

// txsBetween 返回从 ancestor 的下一块走到 tip 的交易，不含 ancestor 自己。
// 顺序和链上一样，从旧块到新块。
func (c *Chain) txsBetween(ancestor, tip Hash) []Tx {
	var groups [][]Tx
	// 从 tip 往回走到 ancestor 就停。先拿到的是较新的块，所以下面要倒过来。
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
	// 倒过来，交易顺序和它们在链上从旧到新的顺序一致。
	for i := len(groups) - 1; i >= 0; i-- {
		out = append(out, groups[i]...)
	}
	return out
}

// nonceKey 把发送方和 nonce 拼成换链头时去重用的键。地址 20 字节在前，nonce 的大端 8 字节在后。
func nonceKey(sender Address, nonce uint64) string {
	var buf [28]byte
	copy(buf[:20], sender[:])
	binary.BigEndian.PutUint64(buf[20:], nonce)
	return string(buf[:])
}
