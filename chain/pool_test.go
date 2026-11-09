package chain

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

// demoKey 返回测试用的演示私钥。
func demoKey(t *testing.T) []byte {
	t.Helper()
	key, err := sig.ParseKey(sig.DemoKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// otherKey 是另一把私钥，用来造第二个发送方。
func otherKey() []byte {
	key := make([]byte, 32)
	key[31] = 1
	return key
}

// 四种进不了池的交易。签名首字节改成 0，恢复不出发送方。
// 账户下一笔该用 1，交易 nonce 却是 0，号码已经用过。
// 转 50 时余额只有 40。转 10 时余额 21009，加上 21000 的 gas 预留还差 1。
func TestPoolRejectsBadSignatureNonceAndBalance(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 1
	good, err := Sign(key, to, 10, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}

	// 签名首字节改成 0。恢复不出发送方。
	badSig := good.Clone()
	badSig.Sig[0] = 0
	pool := NewPool()
	if err := pool.AddTx(badSig, 100000, 0); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("bad signature err = %v", err)
	}

	// 账户下一笔该用 1，这笔 nonce 是 0，号码已经用过。
	low, err := Sign(key, to, 10, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.AddTx(low, 100000, 1); !errors.Is(err, ErrNonceTooLow) {
		t.Fatalf("low nonce err = %v", err)
	}

	// 转 50，余额只有 40。
	poor, err := Sign(key, to, 50, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.AddTx(poor, 40, 1); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("balance err = %v", err)
	}

	// 转 10，再加上 21000 的 gas 预留，余额 21009 差 1。
	fee, err := Sign(key, to, 10, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.AddTx(fee, 21009, 1); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("fee err = %v", err)
	}
}

// gasLimit 取最大值、单价是 2，乘积超出 uint64。
// 另一笔的 gas 预留刚好算得出来，再加上 value 仍然超出。池和 Apply 都拒绝这第二笔。
func TestPoolRejectsFeeOverflow(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 1
	// gasLimit 是最大值，单价 2，乘积放不进 uint64。
	product, err := Sign(key, to, 1, 0, ^uint64(0), 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewPool().AddTx(product, ^uint64(0), 0); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("product err = %v", err)
	}
	// gas 预留刚好算得出来。value 再加 1 就溢出。
	price := ^uint64(0) / IntrinsicGas
	reserved := IntrinsicGas * price
	sum, err := Sign(key, to, ^uint64(0)-reserved+1, 0, IntrinsicGas, price, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewPool().AddTx(sum, ^uint64(0), 0); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("sum err = %v", err)
	}
	// 同一笔交给 Apply，也是余额溢出，不是进了池才能执行。
	st, _ := funded(t, ^uint64(0), 0)
	if _, err := Apply(st, sum); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("apply err = %v", err)
	}
}

// 放入顺序是演示账户的 nonce 1、另一个发送方的 nonce 0、再是演示账户的 nonce 0。
// 取出时先按地址、再按 nonce。同一笔 nonce 0 用相同单价再放一次，不能替换。
// 丢掉排在最前的那笔之后，剩下两笔，被丢掉的号码不再出现。
func TestPoolOrdersBySenderThenNonce(t *testing.T) {
	var to Address
	to[19] = 2
	demo, err := Sign(demoKey(t), to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	demoFirst, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Sign(otherKey(), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 放入顺序故意打乱：nonce 1、另一个发送方、再 nonce 0。
	pool := NewPool()
	for _, tx := range []Tx{demo, other, demoFirst} {
		if err := pool.AddTx(tx, 100000, 0); err != nil {
			t.Fatal(err)
		}
	}
	// 同一笔 nonce 0、同一单价再放一次，不能替换。
	if err := pool.AddTx(demoFirst, 100000, 0); !errors.Is(err, ErrReplacementUnderpriced) {
		t.Fatalf("replacement err = %v", err)
	}
	// 取出来必须先按地址，同一地址再按 nonce 从小到大。
	got := pool.PendingTxs()
	if len(got) != 3 {
		t.Fatalf("pending = %d", len(got))
	}
	senders := make([]Address, len(got))
	for i, tx := range got {
		senders[i], err = tx.Sender()
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			cmp := bytes.Compare(senders[i-1][:], senders[i][:])
			if cmp > 0 || (cmp == 0 && got[i-1].Nonce > tx.Nonce) {
				t.Fatalf("order broken at %d", i)
			}
		}
	}
	// 丢掉排在最前的那一笔。剩下两笔里不能再看到这个发送方的这个号码。
	droppedSender, droppedNonce := senders[0], got[0].Nonce
	pool.DropTx(droppedSender, droppedNonce)
	left := pool.PendingTxs()
	if len(left) != 2 {
		t.Fatalf("after drop pending = %d", len(left))
	}
	for _, tx := range left {
		sender, err := tx.Sender()
		if err != nil {
			t.Fatal(err)
		}
		if sender == droppedSender && tx.Nonce == droppedNonce {
			t.Fatal("dropped transaction is still pending")
		}
	}
}

// 同一发送方、同一 nonce 先放单价 1。单价 2 把它换掉，池里只剩这一笔。
// 再放回单价 1 被拒绝，池里仍然是单价 2 那笔。
func TestPoolReplacesUnderNonceWhenGasPriceIsHigher(t *testing.T) {
	var to Address
	to[19] = 2
	key := demoKey(t)
	cheap, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	pricey, err := Sign(key, to, 1, 0, 21000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewPool()
	if err := pool.AddTx(cheap, 100000, 0); err != nil {
		t.Fatal(err)
	}
	// 单价 2 换掉单价 1。池里只剩这一笔。
	if err := pool.AddTx(pricey, 100000, 0); err != nil {
		t.Fatalf("replace err = %v", err)
	}
	got := pool.PendingTxs()
	if len(got) != 1 || got[0].GasPrice != pricey.GasPrice || !bytes.Equal(got[0].Sig, pricey.Sig) {
		t.Fatalf("pending = %+v", got)
	}
	// 单价更低的换不回来。
	if err := pool.AddTx(cheap, 100000, 0); !errors.Is(err, ErrReplacementUnderpriced) {
		t.Fatalf("underpriced err = %v", err)
	}
	got = pool.PendingTxs()
	if len(got) != 1 || got[0].GasPrice != pricey.GasPrice || !bytes.Equal(got[0].Sig, pricey.Sig) {
		t.Fatalf("after underpriced pending = %+v", got)
	}
}

// 交易进池之后，改调用方手里的签名和 data，池里的副本不变。
// 再改 PendingTxs 返回的签名，下一次取出来仍是池里原来的字节。
func TestPoolCopiesSignatures(t *testing.T) {
	var to Address
	to[19] = 2
	tx, err := SignCall(demoKey(t), to, 1, 0, 30000, 1, ChainID, []byte{0x01})
	if err != nil {
		t.Fatal(err)
	}
	caller := bytes.Clone(tx.Sig)
	data := tx.Data
	tx.Sig = caller
	pool := NewPool()
	if err := pool.AddTx(tx, 100000, 0); err != nil {
		t.Fatal(err)
	}
	// 进池之后改调用方自己的切片。池里应是进池时的副本。
	caller[0] ^= 0xff
	data[0] ^= 0xff
	stored := pool.PendingTxs()
	if len(stored) != 1 || bytes.Equal(stored[0].Sig, caller) || bytes.Equal(stored[0].Data, data) {
		t.Fatal("pool kept the caller's signature or data slice")
	}
	// 改这次取出来的签名。下次再取，应仍是池里原来的字节。
	stored[0].Sig[0] ^= 0xff
	again := pool.PendingTxs()
	if bytes.Equal(again[0].Sig, stored[0].Sig) {
		t.Fatal("pending returned the pool's signature slice")
	}
}

// nonce 0 和 1 都在池里。丢掉 0 之后，这个号码的槽被删掉，只剩下 nonce 1。
func TestDropClearsDiscardedSlots(t *testing.T) {
	var to Address
	to[19] = 2
	first, err := Sign(demoKey(t), to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(demoKey(t), to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewPool()
	for _, tx := range []Tx{first, second} {
		if err := pool.AddTx(tx, 100000, 0); err != nil {
			t.Fatal(err)
		}
	}
	sender, err := first.Sender()
	if err != nil {
		t.Fatal(err)
	}
	// 丢掉 nonce 0。内层表不再留这个号码，只剩 nonce 1。
	pool.DropTx(sender, first.Nonce)
	if _, ok := pool.bySender[sender][first.Nonce]; ok {
		t.Fatal("dropped nonce is still stored")
	}
	left := pool.PendingTxs()
	if len(left) != 1 || left[0].Nonce != second.Nonce {
		t.Fatalf("remaining = %+v", left)
	}
}

// 第一笔 value 979000，加上 21000 的 gas 预留，刚好等于余额 1000000。
// nonce 1 再要同样一笔。两笔从账户 nonce 起连续，合起来超过余额，第二笔进不去。
// 各转 1 的 nonce 0 和 1 预留得下，两笔都进池。
func TestPoolReservesBalanceForOtherPendingTxs(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 2
	first, err := Sign(key, to, 979000, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(key, to, 979000, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 这个例子把余额钉在 1000000，用来卡住两笔预留。创世余额是 1 个币，放得下这两笔。
	const balance uint64 = 1000000
	pool := NewPool()
	if err := pool.AddTx(first, balance, 0); err != nil {
		t.Fatal(err)
	}
	// nonce 0 已经占满余额。连续的 nonce 1 再要同样一笔，进不去。
	if err := pool.AddTx(second, balance, 0); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("second err = %v", err)
	}
	if len(pool.PendingTxs()) != 1 {
		t.Fatal("over-balance transaction was stored")
	}
	one, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Sign(key, to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 各转 1 的两笔连续交易，预留得下。
	affordable := NewPool()
	if err := affordable.AddTx(one, balance, 0); err != nil {
		t.Fatal(err)
	}
	if err := affordable.AddTx(two, balance, 0); err != nil {
		t.Fatal(err)
	}
	// 两笔都在池里，按 nonce 0、1 排列，金额都是 1。
	pending := affordable.PendingTxs()
	if len(pending) != 2 || pending[0].Nonce != 0 || pending[1].Nonce != 1 || pending[0].Value != 1 || pending[1].Value != 1 {
		t.Fatalf("pending = %+v", pending)
	}
	if !bytes.Equal(pending[0].Sig, one.Sig) || !bytes.Equal(pending[1].Sig, two.Sig) {
		t.Fatal("pending transactions are not the two small transfers")
	}
}

// 账户下一笔该用 0。先放 nonce 2，前面有空号，可执行号码不动，仍是 0。
// 补上 nonce 0 后，下一笔是 1。再补 nonce 1，连续段穿过已经在池里的 nonce 2，下一笔变成 3。
func TestPendingNonceStopsAtGap(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 2
	future, err := Sign(key, to, 1, 2, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(key, to, 1, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewPool()
	sender, err := future.Sender()
	if err != nil {
		t.Fatal(err)
	}
	// 先放 nonce 2。前面有空号，可执行号码不往前推。
	if err := pool.AddTx(future, 100000, 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := pool.pendingNonce[sender]; ok || pool.NextNonce(sender, 0) != 0 {
		t.Fatalf("gap advanced pending nonce to %d", pool.pendingNonce[sender])
	}
	// 补上 nonce 0。下一笔是 1，还没连上已经在池里的 2。
	if err := pool.AddTx(first, 100000, 0); err != nil {
		t.Fatal(err)
	}
	if pool.pendingNonce[sender] != 1 || pool.NextNonce(sender, 0) != 1 {
		t.Fatalf("pending nonce = %d", pool.pendingNonce[sender])
	}
	// 再补 nonce 1。连续段穿过 nonce 2，下一笔变成 3。
	if err := pool.AddTx(second, 100000, 0); err != nil {
		t.Fatal(err)
	}
	if pool.pendingNonce[sender] != 3 || pool.NextNonce(sender, 0) != 3 {
		t.Fatalf("pending nonce = %d", pool.pendingNonce[sender])
	}
}

// 每笔 value 加 21000 是 400000，三笔加起来超过余额 1000000。
// nonce 是 0、2、3，中间缺 1。预留只累加从账户 nonce 起的连续段，空号后面的不并进这笔总和。
// 所以三笔都进得去。
func TestPoolDoesNotReserveBalanceAcrossNonceGap(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 2
	// 每笔费用是 value + 21000。三笔各 400000，加起来超过余额，但只有 nonce 0 和后面断开的号码。
	const (
		balance uint64 = 1000000
		value   uint64 = 379000
	)
	first, err := Sign(key, to, value, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	gap, err := Sign(key, to, value, 2, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	further, err := Sign(key, to, value, 3, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 0、2、3 中间缺 1。空号后面的不并进连续段的预留，三笔都进得去。
	pool := NewPool()
	for _, tx := range []Tx{first, gap, further} {
		if err := pool.AddTx(tx, balance, 0); err != nil {
			t.Fatalf("nonce %d err = %v", tx.Nonce, err)
		}
	}
	if len(pool.PendingTxs()) != 3 {
		t.Fatalf("pending = %d", len(pool.PendingTxs()))
	}
}

// nonce 0 转 1、单价 1，加上 nonce 1 转 500000，余额 1000000 放得下。
// 用单价 2、金额 500000 替换 nonce 0。替换后的预留加上 nonce 1 超过余额，拒绝。
// 池里仍是替换前的两笔。
func TestPoolReplacementMustStillFitWithSiblings(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 2
	cheap, err := Sign(key, to, 1, 0, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := Sign(key, to, 500000, 1, 21000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	tooBig, err := Sign(key, to, 500000, 0, 21000, 2, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 替换后的预留加上同账户另一笔，会超过这个 1000000。创世余额放得下。
	const balance uint64 = 1000000
	pool := NewPool()
	if err := pool.AddTx(cheap, balance, 0); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddTx(sibling, balance, 0); err != nil {
		t.Fatal(err)
	}
	// 替换 nonce 0 之后，新预留加上 nonce 1 超过余额。原来的两笔留着。
	if err := pool.AddTx(tooBig, balance, 0); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("replacement err = %v", err)
	}
	pending := pool.PendingTxs()
	if len(pending) != 2 || pending[0].GasPrice != 1 || pending[0].Nonce != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}

// 区间两端相同，没有需要检查的号码，filled 为真。
// 池里只有 nonce 0，pendingNonce 却写成 4。0 到 4 中间有空号，filled 为假。
// NextNonce 不信这条记录，从链头重新数，下一笔是 1。
// nonce 已经是最大值时，下一笔停在最大值，不会加 1 绕回 0。
func TestPoolFilledAndMaxNonce(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 2
	sender, err := Sign(key, to, 1, 0, 21000, 0, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	from, err := sender.Sender()
	if err != nil {
		t.Fatal(err)
	}
	pool := NewPool()
	// 3 到 3 是空区间，没有缺号。
	if !pool.filled(from, 3, 3) {
		t.Fatal("empty range")
	}
	if err := pool.AddTx(sender, 100000, 0); err != nil {
		t.Fatal(err)
	}
	// 记录写成 4，池里却只有 nonce 0。中间缺号，不能当成已经填满。
	pool.pendingNonce[from] = 4
	if pool.filled(from, 0, 4) {
		t.Fatal("gap should be reported")
	}
	// 记录和池对不上，从账户 nonce 0 重新数。只有 nonce 0，下一笔是 1。
	if pool.NextNonce(from, 0) != 1 {
		t.Fatalf("recount = %d", pool.NextNonce(from, 0))
	}

	// nonce 取最大值。promoteNonce 停在这一笔，不再加 1。
	maxTx, err := Sign(key, to, 1, math.MaxUint64, 21000, 0, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	maxPool := NewPool()
	if err := maxPool.AddTx(maxTx, 100000, math.MaxUint64); err != nil {
		t.Fatal(err)
	}
	if maxPool.NextNonce(from, math.MaxUint64) != math.MaxUint64 {
		t.Fatalf("max nonce = %d", maxPool.NextNonce(from, math.MaxUint64))
	}
}

// 两笔金额都是 MaxUint64/2+2，单价是 0，费用就是金额。第一笔进池，和第二笔相加超出 uint64。
// nonce 0 转 10，nonce 2 转 MaxUint64-5，中间缺 1。空号后面这笔仍加在连续段后面，10 再加上它超出。
// 绕过检查直接放入 gasLimit 最大、单价 2 的交易。费用乘积已经超出，再进下一笔时累加到它就拒绝。
// 池里先放着 nonce 最大值。再放入前一号时，这个号码是空的，连续段在这里停下，nonce 不会加过最大值绕回 0。
func TestPoolBalanceFitsOverflow(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 2
	first, err := Sign(key, to, math.MaxUint64/2+2, 0, 21000, 0, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sign(key, to, math.MaxUint64/2+2, 1, 21000, 0, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 第一笔进得去。再加上同样大的 nonce 1，两笔金额之和超出 uint64。
	pool := NewPool()
	if err := pool.AddTx(first, math.MaxUint64, 0); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddTx(second, math.MaxUint64, 0); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("sum err = %v", err)
	}

	small, err := Sign(key, to, 10, 0, 21000, 0, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	future, err := Sign(key, to, math.MaxUint64-5, 2, 21000, 0, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 连续段只有 nonce 0 的 10。nonce 2 落在空号后面，仍要加到这段后面，10 加 MaxUint64-5 超出。
	gapped := NewPool()
	if err := gapped.AddTx(small, math.MaxUint64, 0); err != nil {
		t.Fatal(err)
	}
	if err := gapped.AddTx(future, math.MaxUint64, 0); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("gap sum err = %v", err)
	}

	from, err := first.Sender()
	if err != nil {
		t.Fatal(err)
	}
	// 这笔 gas 上限乘单价已经超出。storeTx 不检查费用，下一笔累加时才碰到它。
	stored := NewPool()
	bad := first.Clone()
	bad.GasLimit = math.MaxUint64
	bad.GasPrice = 2
	stored.storeTx(from, bad)
	next, err := Sign(key, to, 1, 1, 21000, 0, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if err := stored.AddTx(next, math.MaxUint64, 0); !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("stored overflow err = %v", err)
	}

	maxPrev, err := Sign(key, to, 1, math.MaxUint64-1, 21000, 0, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	maxTx, err := Sign(key, to, 1, math.MaxUint64, 21000, 0, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 最大值已经在池里。前一号是空的，从这里停下，不会把 nonce 加过最大值。金额很小，可以进池。
	wrap := NewPool()
	wrap.storeTx(from, maxTx)
	if err := wrap.AddTx(maxPrev, 100000, math.MaxUint64-1); err != nil {
		t.Fatal(err)
	}
}
