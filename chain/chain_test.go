package chain

import (
	"bytes"
	"errors"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

// 这个文件测出块、接块、交易池和区块哈希。分叉选择在 fork_test.go。
// Mine 按发送方排队：单价高的先装，单价相同则地址更小的先装。占容量看 gasLimit，收费看实际烧掉的 gas。

// 创世块高度是 0，父哈希全 0，没有交易。
// 演示账户有 1 个币，nonce 是 0。状态根等于只放这个账户算出来的根。
func TestNewGenesisFundsDemoAccount(t *testing.T) {
	c := New()
	// 创世块还没有父块，也没有交易。
	head := c.Head()
	if head.Height != 0 || head.ParentHash != (Hash{}) || len(head.Txs) != 0 {
		t.Fatalf("genesis = %+v", head)
	}
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	var addr Address
	copy(addr[:], raw[:])
	// 演示账户有 1 个币。状态根按这个账户单独算一遍，要和区块头里的相同。
	acct, ok := c.Account(addr)
	if !ok || acct.Balance != GenesisBalance || acct.Nonce != 0 {
		t.Fatalf("demo = %+v ok=%v", acct, ok)
	}
	st := NewState()
	st.SetAccount(acct)
	if head.StateRoot != StateRoot(st) {
		t.Fatalf("genesis root = %s", head.StateRoot.Hex())
	}
}

// 池是空的，下一笔是 0。先放 nonce 2，前面有空号，下一笔仍是 0。
// 补上 nonce 0 后，下一笔是 1。再补 nonce 1，连续段穿过已有的 2，下一笔变成 3。
func TestNextNonceSkipsPending(t *testing.T) {
	c := New()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	var sender Address
	copy(sender[:], raw[:])
	// 池是空的，下一笔就是账户上的 0。
	if c.NextNonce(sender) != 0 {
		t.Fatal("empty next nonce")
	}
	var to Address
	to[19] = 1
	future, err := Sign(key, to, 1, 2, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(future); err != nil {
		t.Fatal(err)
	}
	// nonce 2 前面有空号，下一笔仍是 0。
	if c.NextNonce(sender) != 0 {
		t.Fatal("gap should stay open")
	}
	first, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(first); err != nil {
		t.Fatal(err)
	}
	// nonce 0 占住了。下一笔是 1，还没连上池里的 2。
	if c.NextNonce(sender) != 1 {
		t.Fatal("nonce 0 is taken")
	}
	second, err := Sign(key, to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(second); err != nil {
		t.Fatal(err)
	}
	// 补上 nonce 1 之后，连续段穿过 2，下一笔是 3。
	if got := c.NextNonce(sender); got != 3 {
		t.Fatalf("next = %d", got)
	}
}

// 一笔转账出块后离开池，收款方入账。跟随者接上后，链头哈希和状态根与出块的一方相同。
// 改掉父哈希或状态根再接，两笔都被拒绝。状态根被改过的那次，链头仍停在创世块。
func TestMineAndAppend(t *testing.T) {
	c := New()
	parent := c.Head().Hash()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[19] = 8
	tx, err := Sign(key, to, 15, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	// 时间必须晚于父块。创世时间是 0，这里用 Head().Time+1。
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if block.Height != 1 || block.ParentHash != parent || len(block.Txs) != 1 {
		t.Fatalf("mined = %+v", block)
	}
	// 交易进了块，就从池里删掉。收款方在链头状态上入账。
	if len(c.Pool().PendingTxs()) != 0 {
		t.Fatal("mined transaction still pending")
	}
	acct, _ := c.Account(to)
	if acct.Balance != U256(15) {
		t.Fatalf("recipient balance = %d", acct.Balance)
	}

	// 另一条链从同一个创世块接上这块。
	follower := New()
	if err := follower.Append(block); err != nil {
		t.Fatal(err)
	}
	if follower.Head().Hash() != c.Head().Hash() || follower.Head().StateRoot != c.Head().StateRoot {
		t.Fatal("follower diverged")
	}

	// 父哈希或状态根改一个字节，块就接不上。
	badParent := block
	badParent.ParentHash[0] ^= 0x01
	if err := New().Append(badParent); err == nil {
		t.Fatal("expected bad parent to fail")
	}
	// 父块对得上，但状态根被改过。重放结果对不上，块不保存。
	badRoot := block
	badRoot.StateRoot[0] ^= 0x01
	fresh := New()
	if err := fresh.Append(badRoot); err == nil {
		t.Fatal("expected bad root to fail")
	}
	if fresh.Head().Height != 0 {
		t.Fatal("rejected block was kept")
	}
}

// 池里只有 nonce 1，接不上账户上的 0。出块仍然进行，这块没有交易，那笔留在池里。
func TestMineLeavesGappedNonce(t *testing.T) {
	c := New()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[0] = 1
	// 账户 nonce 仍是 0。池允许未来号码，出块时这一队号码对不上就关掉。
	future, err := Sign(key, to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(future); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if block.Height != 1 || len(block.Txs) != 0 {
		t.Fatalf("empty block = height %d txs %d", block.Height, len(block.Txs))
	}
	// 空号后面的交易不能装进这块，所以还在池里。
	if len(c.Pool().PendingTxs()) != 1 {
		t.Fatal("gapped transaction was dropped")
	}
}

// 跟随者池里先有这笔交易。接上已经包含它的块后，它从池里删掉。
// 同一块再接一次，高度仍是 1，池也仍是空的。
func TestAppendDropsIncludedTx(t *testing.T) {
	producer := New()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[1] = 2
	tx, err := Sign(key, to, 4, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := producer.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := producer.Mine(Address{}, producer.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	// 跟随者先把同一笔放进自己的池，再接已经包含它的块。
	follower := New()
	if _, err := follower.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	if err := follower.Append(block); err != nil {
		t.Fatal(err)
	}
	if len(follower.Pool().PendingTxs()) != 0 {
		t.Fatal("appended transaction still pending")
	}
	// 同一块再来一次。高度和池都不再变化。
	if err := follower.Append(block); err != nil {
		t.Fatal(err)
	}
	if follower.Head().Height != 1 || len(follower.Pool().PendingTxs()) != 0 {
		t.Fatal("duplicate append changed the chain")
	}
}

// 挖出高度 1 的块后，把高度改成 2 再交给还在创世块的跟随者。
// 高度不是父块加 1，拒绝。跟随者的链头不动。
func TestAppendRejectsSkippedHeight(t *testing.T) {
	c := New()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	var to Address
	to[19] = 8
	tx, err := Sign(key, to, 15, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}

	// 高度改成 2，父块仍是创世块。中间缺了高度 1。
	skipped := block
	skipped.Height = 2
	follower := New()
	// Append 要求高度等于父块加 1。改高度也会改哈希，但这块仍指向创世父块。
	if err := follower.Append(skipped); err == nil {
		t.Fatal("expected skipped height to fail")
	}
	if follower.Head().Height != 0 {
		t.Fatal("follower advanced past genesis")
	}
}

// 先给第二个账户转入一笔，两个发送方都有余额。
// 地址更小的挂两笔，gasLimit 是 60000 和 50000。地址更大的挂一笔 21000。单价都是 1。
// 区块上限 100000。地址更小的先装 60000，它的下一笔 50000 超过剩余容量，这一队停住。
// 另一队的 21000 还装得下。实际烧掉两笔各 21000，coinbase 收到 42000。50000 那笔留在池里。
func TestMineStopsWhenBlockGasIsFull(t *testing.T) {
	c := New()
	demo := demoKey(t)
	other := otherKey()
	otherRaw, err := sig.KeyAddress(other)
	if err != nil {
		t.Fatal(err)
	}
	var otherAddr Address
	copy(otherAddr[:], otherRaw[:])
	demoRaw, err := sig.KeyAddress(demo)
	if err != nil {
		t.Fatal(err)
	}
	var demoAddr Address
	copy(demoAddr[:], demoRaw[:])

	// 创世时只有演示账户有余额。先转一笔给 other，后面两个发送方才能各自出交易。
	fund, err := Sign(demo, otherAddr, 400000, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(fund); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mine(Address{}, c.Head().Time+1); err != nil {
		t.Fatal(err)
	}

	var sink Address
	sink[19] = 4
	// 刚挖完：demo 的 nonce 已经是 1，other 仍是 0。默认先假定 demo 地址更小。
	earlierKey, earlierNonce := demo, uint64(1)
	laterKey, laterNonce := other, uint64(0)
	// 单价相同，出块时地址字节更小的先装。把两笔大 gasLimit 挂在更小的那个地址上。
	if bytes.Compare(otherAddr[:], demoAddr[:]) < 0 {
		earlierKey, earlierNonce = other, 0
		laterKey, laterNonce = demo, 1
	}
	first, err := Sign(earlierKey, sink, 1, earlierNonce, 60000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(earlierKey, sink, 1, earlierNonce+1, 50000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	third, err := Sign(laterKey, sink, 1, laterNonce, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 进池顺序不影响出块：池按地址再按 nonce 排，Mine 再按单价和地址选。
	for _, tx := range []Tx{second, third, first} {
		if _, err := c.AddTx(tx); err != nil {
			t.Fatal(err)
		}
	}
	var coin Address
	coin[19] = 3
	block, err := c.Mine(coin, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if block.GasLimit != BlockGasLimit {
		t.Fatalf("gas limit = %d", block.GasLimit)
	}
	// 先装 60000，剩余 40000。同一队下一笔 50000 装不下，这一队关掉。
	// 另一队 21000 还装得下。占容量按 gasLimit，gasUsed 按实际烧掉的 21000+21000。
	if block.GasUsed != 42000 || len(block.Txs) != 2 {
		t.Fatalf("gas used = %d txs = %d", block.GasUsed, len(block.Txs))
	}
	// 被关掉的那一队后面不再看，50000 那笔留在池里。
	pending := c.Pool().PendingTxs()
	if len(pending) != 1 || pending[0].GasLimit != 50000 || pending[0].Nonce != earlierNonce+1 {
		t.Fatalf("pending = %+v", pending)
	}
	// 单价 1，实际烧掉 42000，费用全部进本块 coinbase。
	paid, ok := c.Account(coin)
	if !ok || paid.Balance != U256(42000) {
		t.Fatalf("coinbase = %+v ok=%v", paid, ok)
	}
}

// 同一份区块头只改 gasLimit、gasUsed、高度或时间。四种改法算出来的哈希都和原来不同。
func TestBlockHashBindsGas(t *testing.T) {
	// 哈希原文含 gasLimit、gasUsed、高度和时间，不含交易和回执列表。
	base := Block{ParentHash: Hash{1}, StateRoot: Hash{2}, Coinbase: Address{19: 1}}
	// 四个字段各改一处。哈希要把它们都算进去。
	limit := base
	limit.GasLimit = BlockGasLimit
	used := base
	used.GasUsed = IntrinsicGas
	tall := base
	tall.Height = 1
	late := base
	late.Time = 1
	if base.Hash() == limit.Hash() || base.Hash() == used.Hash() || base.Hash() == tall.Hash() || base.Hash() == late.Hash() {
		t.Fatal("hash ignored gas, height, or timestamp")
	}
}

// 两笔转账挖进同一块。回执的累计 gas 是 21000，然后是 42000。
// 交易根和回执根都按区块体里的内容重算。拿掉交易原文或回执列表，哈希不变。改回执根的一个字节，哈希就变。
func TestMineReceiptsRootMatchesTransactions(t *testing.T) {
	c := New()
	key := demoKey(t)
	var to Address
	to[19] = 9
	// nonce 0 转 4，nonce 1 转 6。同一发送方按 nonce 顺序装进同一块。
	for nonce, value := range []uint64{4, 6} {
		tx, err := Sign(key, to, value, uint64(nonce), 21000, 1, ChainID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.AddTx(tx); err != nil {
			t.Fatal(err)
		}
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	// 按普通转账重算。第一笔累计 21000，第二笔累计 42000。
	recomputed := receiptsFromTransfers(block.Txs)
	if len(recomputed) != 2 || recomputed[0].CumulativeGasUsed != 21000 || recomputed[1].CumulativeGasUsed != 42000 {
		t.Fatalf("recomputed = %+v", recomputed)
	}
	if block.ReceiptsRoot != ReceiptsRoot(recomputed) {
		t.Fatalf("receipts root = %s", block.ReceiptsRoot.Hex())
	}
	if block.TransactionsRoot != TransactionsRoot(block.Txs) {
		t.Fatalf("transactions root = %s", block.TransactionsRoot.Hex())
	}
	// 交易和回执的原文不进哈希，三棵树的根进哈希。
	withoutTxs := block
	withoutTxs.Txs = nil
	if withoutTxs.Hash() != block.Hash() {
		t.Fatal("hash included the raw transactions")
	}
	flipped := block
	flipped.ReceiptsRoot[0] ^= 0x01
	if flipped.Hash() == block.Hash() {
		t.Fatal("hash ignored receipts root")
	}
	withoutList := block
	withoutList.Receipts = nil
	if withoutList.Hash() != block.Hash() {
		t.Fatal("hash included the receipt list")
	}
	// 区块体里的回执列表仍要和按交易重算的一致，只是不算进哈希。
	if len(block.Receipts) != len(recomputed) {
		t.Fatalf("receipts = %+v", block.Receipts)
	}
	for i := range recomputed {
		got, want := block.Receipts[i], recomputed[i]
		if got.Status != want.Status || got.GasUsed != want.GasUsed || got.CumulativeGasUsed != want.CumulativeGasUsed || len(got.Logs) != len(want.Logs) {
			t.Fatalf("receipt %d = %+v", i, got)
		}
	}
}

// 挖出一块后改回执根的一个字节。跟随者重放出来的回执根对不上，拒绝整块。
// 链头仍是创世块，收款方账户也没有建出来。
func TestAppendRejectsBadReceiptsRoot(t *testing.T) {
	c := New()
	key := demoKey(t)
	var to Address
	to[19] = 8
	tx, err := Sign(key, to, 15, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	// 回执根本身改一个字节。重放算出来的根对不上。
	block.ReceiptsRoot[0] ^= 0x01
	follower := New()
	// 交易能重放成功，但算出来的回执根对不上头里写的，整块拒绝，状态不写。
	if err := follower.Append(block); err == nil {
		t.Fatal("expected bad receipts root to fail")
	}
	if follower.Head().Height != 0 {
		t.Fatal("follower advanced past genesis")
	}
	if _, ok := follower.Account(to); ok {
		t.Fatal("rejected block changed state")
	}
}

// 创世块时间是 0。用 0 挖，不晚于父块，拒绝。用 1 挖成功。
// 再挖一块时间仍是 1，等于父块时间，同样拒绝。
func TestMineRejectsTimestampNotAfterParent(t *testing.T) {
	c := New()
	// 创世时间是 0。新块时间不大于它，不能接上去。
	if _, err := c.Mine(Address{}, 0); err == nil || err.Error() != "bad timestamp" {
		t.Fatalf("timestamp err = %v", err)
	}
	if _, err := c.Mine(Address{}, 1); err != nil {
		t.Fatal(err)
	}
	// 父块时间已经是 1。再挖一块时间仍是 1，也不行。
	if _, err := c.Mine(Address{}, 1); err == nil || err.Error() != "bad timestamp" {
		t.Fatalf("equal timestamp err = %v", err)
	}
}

// 同一块分别改时间、gasLimit 和 gasUsed。时间不晚于父块、gasLimit 不是 100000、
// gasUsed 和重放结果不一致，三次都拒绝，链头不动。未改过的原块可以接上。
func TestAppendChecksTimestampGasLimitAndGasUsed(t *testing.T) {
	c := New()
	// 空块也能出。时间 1 晚于创世的 0。
	block, err := c.Mine(Address{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	follower := New()
	// 改这些字段会改哈希，所以三次都是未知新块。时间、区块 gas 上限、实际消耗各错一次。
	early := block
	early.Time = 0
	if err := follower.Append(early); err == nil || err.Error() != "bad timestamp" {
		t.Fatalf("timestamp err = %v", err)
	}
	limited := block
	limited.GasLimit = 1
	// 区块 gas 上限必须是 BlockGasLimit，不能自己改成别的数。
	if err := follower.Append(limited); err == nil || err.Error() != "bad gas limit" {
		t.Fatalf("gas limit err = %v", err)
	}
	used := block
	used.GasUsed = 1
	// 空块重放后实际消耗是 0，头里写成 1 对不上。
	if err := follower.Append(used); err == nil || err.Error() != "bad gas used" {
		t.Fatalf("gas used err = %v", err)
	}
	if follower.Head().Height != 0 {
		t.Fatal("rejected block advanced the head")
	}
	// 三次都没改链头。原来的块仍能接上。
	if err := follower.Append(block); err != nil {
		t.Fatal(err)
	}
	if follower.Head().Height != block.Height {
		t.Fatalf("head = %d want %d", follower.Head().Height, block.Height)
	}
}

// 两笔交易的 gasLimit 都是 60000，加起来超过区块上限 100000。
// Append 拒绝整块。链头仍是创世块，这块也不保存。
func TestAppendRejectsGasLimitSumOverBlock(t *testing.T) {
	c := New()
	var to Address
	to[19] = 4
	first, err := Sign(demoKey(t), to, 1, 0, 60000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(demoKey(t), to, 1, 1, 60000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 不经过 Mine，直接拼一块。头里 gasLimit 仍是 100000，两笔声明的上限加起来是 120000。
	parent := c.Head()
	block := Block{
		Height:           parent.Height + 1,
		ParentHash:       parent.Hash(),
		GasLimit:         BlockGasLimit,
		GasUsed:          IntrinsicGas * 2,
		Time:             parent.Time + 1,
		Txs:              []Tx{first, second},
		TransactionsRoot: TransactionsRoot([]Tx{first, second}),
	}
	if err := c.Append(block); err == nil || err.Error() != "block gas exceeded" {
		t.Fatalf("err = %v", err)
	}
	// 容量检查在重放写入之前。链头不动，这块也不进 blocks。
	if c.Head().Height != 0 || c.HasBlock(block.Hash()) {
		t.Fatal("over-gas block was stored")
	}
}

// 两个发送方各挂一笔 gasLimit 60000 的交易，单价是 1 和 5。两笔一起超过区块上限。
// 先给可能地址更大的那个账户备好余额。出块时单价 5 的先装，块里只有这一笔。
func TestMinePacksHigherGasPriceFirst(t *testing.T) {
	c := New()
	demo := demoKey(t)
	other := otherKey()
	demoRaw, err := sig.KeyAddress(demo)
	if err != nil {
		t.Fatal(err)
	}
	otherRaw, err := sig.KeyAddress(other)
	if err != nil {
		t.Fatal(err)
	}
	var demoAddr, otherAddr Address
	copy(demoAddr[:], demoRaw[:])
	copy(otherAddr[:], otherRaw[:])
	// 先给 other 备余额。挖完后 demo nonce 是 1，other 是 0。
	fund, err := Sign(demo, otherAddr, 400000, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(fund); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mine(Address{}, c.Head().Time+1); err != nil {
		t.Fatal(err)
	}
	cheapKey, priceyKey := demo, other
	cheapNonce, priceyNonce := uint64(1), uint64(0)
	// 把单价 5 的那笔放到地址更大的发送方上。若只比地址，它会更晚；单价更高时仍应先装。
	if bytes.Compare(demoAddr[:], otherAddr[:]) > 0 {
		priceyKey, cheapKey = demo, other
		priceyNonce, cheapNonce = 1, 0
	}
	var sink Address
	sink[19] = 7
	cheap, err := Sign(cheapKey, sink, 1, cheapNonce, 60000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	pricey, err := Sign(priceyKey, sink, 1, priceyNonce, 60000, 5, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tx := range []Tx{cheap, pricey} {
		if _, err := c.AddTx(tx); err != nil {
			t.Fatal(err)
		}
	}
	block, err := c.Mine(Address{19: 3}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	// 单价 5 先装，剩余 40000。单价 1 的 60000 装不下，这一队关掉。块里只留单价 5。
	if len(block.Txs) != 1 || block.Txs[0].GasPrice != 5 {
		t.Fatalf("packed %+v", block.Txs)
	}
}

// 用创世块的哈希能取回它。一个没出现过的哈希取不到块。
func TestBlockReturnsStoredBlock(t *testing.T) {
	c := New()
	head := c.Head()
	got, ok := c.BlockByHash(head.Hash())
	if !ok || got.Height != 0 || got.StateRoot != head.StateRoot {
		t.Fatalf("genesis block = %+v ok=%v", got, ok)
	}
	// 这个哈希从没出现过，取不到块。
	if _, ok := c.BlockByHash(Hash{1}); ok {
		t.Fatal("unknown hash returned a block")
	}
}

// 交易进池后按发送方和 nonce 丢掉，再丢一次也没有这笔。
// 接着出块。块是空的，账户 nonce 和余额仍是创世时的值。
func TestDropTxKeepsTransactionOutOfTheBlock(t *testing.T) {
	c := New()
	key := demoKey(t)
	sender := demoAddr(t)
	var to Address
	to[19] = 9
	tx, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	// 丢掉一次之后池里已经没有这笔。再丢一次也不该报错。
	c.DropTx(sender, 0)
	c.DropTx(sender, 0)
	if len(c.Pool().PendingTxs()) != 0 {
		t.Fatalf("pending = %d", len(c.Pool().PendingTxs()))
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(block.Txs) != 0 {
		t.Fatalf("txs = %d", len(block.Txs))
	}
	// DropTx 只动池，不动链上账户。出空块也不改演示账户的 nonce 和余额。
	acct, _ := c.Account(sender)
	if acct.Nonce != 0 || acct.Balance != GenesisBalance {
		t.Fatalf("account = %+v", acct)
	}
}

// 签名第一字节改成 0，AddTx 恢复不出发送方，返回 ErrBadSignature。
// 同一笔 nonce 0 先入池再出块。链上 nonce 变成 1 之后，再交同样的 nonce 0，池返回 ErrNonceTooLow。
func TestAddTxRejectsBadSignatureAndPoolError(t *testing.T) {
	c := New()
	key := demoKey(t)
	var to Address
	to[19] = 9
	tx, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	bad := tx.Clone()
	bad.Sig[0] = 0
	// 签名坏了，这笔进不了池。
	if _, err := c.AddTx(bad); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("sig err = %v", err)
	}
	used, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(used); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Mine(Address{}, c.Head().Time+1); err != nil {
		t.Fatal(err)
	}
	again, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// nonce 0 已经入块。再交同一号码，池拒绝。
	if _, err := c.AddTx(again); !errors.Is(err, ErrNonceTooLow) {
		t.Fatalf("pool err = %v", err)
	}
}

// 先挖出带一笔交易的块，再把交易根第一字节翻掉，交易体不动。
// 另一条链接这块时，交易根和区块体对不上，返回 bad transactions root。
func TestAppendRejectsBadTransactionsRoot(t *testing.T) {
	c := New()
	key := demoKey(t)
	var to Address
	to[19] = 8
	tx, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	// 只改头里的交易根。Append 先拿交易体重算，对不上就拒绝。
	block.TransactionsRoot[0] ^= 0x01
	if err := New().Append(block); err == nil || err.Error() != "bad transactions root" {
		t.Fatalf("err = %v", err)
	}
}

// 收款方余额已经是 2^256-1。再转入 1 会溢出，Apply 返回错误。出块关掉这位，块是空的。
// 绕过 AddTx，把只有 1 字节签名的交易直接放进池。出块恢复发送方失败，整次 Mine 返回错误。
func TestMineSkipsApplyErrorAndBadPendingSignature(t *testing.T) {
	c := New()
	var to Address
	to[19] = 9
	c.mu.Lock()
	c.state.SetAccount(Account{Address: to, Balance: MaxUint256()})
	c.mu.Unlock()
	tx, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(tx); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	// 收款方再收 1 就会超出 2^256-1。Apply 失败只关掉这位。这里还没有装上任何一笔，所以块是空的。
	if len(block.Txs) != 0 {
		t.Fatalf("overflow tx was mined: %d", len(block.Txs))
	}

	broken := New()
	sender := demoAddr(t)
	// 签名只有 1 字节，AddTx 不会收下。直接写进池，Mine 拆队时恢复发送方失败。
	bad := Tx{To: to, Nonce: 0, GasLimit: 21000, GasPrice: 1, ChainID: ChainID, Sig: []byte{1}}
	broken.mu.Lock()
	broken.pool.storeTx(sender, bad)
	broken.mu.Unlock()
	if _, err := broken.Mine(Address{}, broken.Head().Time+1); err == nil {
		t.Fatal("expected bad pending signature")
	}
}

// adopt 的目标就是当前链头时什么都不做。
// 一个没出现过的哈希和创世块求共同祖先，得到创世块。
// 从创世块走到这个缺失哈希，或反过来走到创世块，都没有交易。
func TestAdoptAndMissingBlockHelpers(t *testing.T) {
	c := New()
	c.mu.Lock()
	defer c.mu.Unlock()
	// 链头没变。
	c.adopt(c.canonical)
	missing := Hash{1}
	// 缺失的一边取不到块，在高度 0 处停下，共同祖先是创世块。
	if got := c.commonAncestor(missing, c.canonical); got != c.canonical {
		t.Fatalf("ancestor = %s", got.Hex())
	}
	// 缺失哈希取不到块，两条方向都走不出交易。
	if txs := c.txsBetween(c.canonical, missing); len(txs) != 0 {
		t.Fatalf("missing tip txs = %d", len(txs))
	}
	if txs := c.txsBetween(missing, c.canonical); len(txs) != 0 {
		t.Fatalf("genesis txs = %d", len(txs))
	}
}

// receiptsFromTransfers 按普通转账重算回执。每笔消耗 21000，累计值逐笔相加。
func receiptsFromTransfers(txs []Tx) []Receipt {
	out := make([]Receipt, len(txs))
	// 普通转账每笔固定 21000。累计值是前面各笔再加上这一笔。
	var cumulative uint64
	for i := range txs {
		cumulative += IntrinsicGas
		out[i] = Receipt{Status: 1, GasUsed: IntrinsicGas, CumulativeGasUsed: cumulative}
	}
	return out
}
