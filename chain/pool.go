package chain

import (
	"bytes"
	"sort"
)

// Pool 是内存里的交易池。同一发送方可以先挂上未来的 nonce，出块时再按连续 nonce 取出。
// bySender 的外层键是发送方，内层键是交易的 nonce。同一 nonce 只有一笔。
// 需要顺序时由 PendingTxs 函数另做一份，按地址再按 nonce 排序。
type Pool struct {
	bySender map[Address]map[uint64]Tx
	// pendingNonce 是每个发送方下一个还能执行的号码，对应以太坊的 pendingNonces。
	// 「只有从账户 nonce 起连续」占住的交易才会把它往后推。「空号后面的交易不改它」。
	pendingNonce map[Address]uint64
}

func NewPool() *Pool {
	return &Pool{
		bySender:     make(map[Address]map[uint64]Tx),
		pendingNonce: make(map[Address]uint64),
	}
}

// AddTx 把交易放进池。
// balance、nonce 是链头上这个账户的余额和下一笔该用的号码，不是交易里的 tx.Nonce。
// 号码从 0 起算，所以 nonce 也等于已经入块的笔数。还没入块时它是 0，第一笔的 tx.Nonce 必须是 0。
// tx.Nonce 更小说明已经用过，拒绝。更大可以先挂着，等前面的号码入块后再执行。
// 从账户 nonce 起、中间没有空号的交易也占用余额。号码断开之后的不计入这笔总和。
// 同一 tx.Nonce 已在池里时，只有 gasPrice 更高才替换。
func (p *Pool) AddTx(tx Tx, balance Uint256, nonce uint64) error {
	sender, err := tx.Sender()
	if err != nil {
		return ErrBadSignature
	}
	// check gasLimit & balance
	if err := checkTxAdmission(tx, balance); err != nil {
		return err
	}
	if tx.Nonce < nonce {
		return ErrNonceTooLow
	}
	if err := p.senderBalanceFits(sender, tx, balance, nonce); err != nil {
		return err
	}
	txs := p.bySender[sender]
	if old, ok := txs[tx.Nonce]; ok {
		// nonce 相同的交易，只有 gasPrice 更高才替换旧交易
		if tx.GasPrice <= old.GasPrice {
			return ErrReplacementUnderpriced
		}
	}
	p.storeTx(sender, tx)
	p.promoteNonce(sender, tx.Nonce, nonce)
	return nil
}

// storeTx 放进这个发送方的 nonce 槽。Clone 后，调用方改自己的 Data 或 Sig 不会改到池里。
func (p *Pool) storeTx(sender Address, tx Tx) {
	txs := p.bySender[sender]
	if txs == nil {
		txs = make(map[uint64]Tx)
		p.bySender[sender] = txs
	}
	txs[tx.Nonce] = tx.Clone()
}

// NextNonce 返回这个发送方下一个还能执行的号码。
// nonce 与 AddTx 相同，是链头上下一笔该用的号。池是空的就返回它。
// 记下的 pendingNonce 已经超过它、并且中间没有空号时直接用记录，否则从链头重新数。
func (p *Pool) NextNonce(sender Address, nonce uint64) uint64 {
	if next, ok := p.pendingNonce[sender]; ok && next >= nonce && p.filled(sender, nonce, next) {
		return next
	}
	return p.promoteNonce(sender, nonce, nonce)
}

// promoteNonce 维护 pendingNonce。这笔正好接在可执行号码上时，记成连续段末尾的下一个号。
// 号码比可执行号码更大时留在空号后面，不推动记录。替换连续段里的旧交易也不改变它。
func (p *Pool) promoteNonce(sender Address, txNonce, accountNonce uint64) uint64 {
	next, ok := p.pendingNonce[sender]
	// 没有记录、链头已经超过记录，或记录到链头之间出现空号时，从账户 nonce 重新数。
	if !ok || next < accountNonce || !p.filled(sender, accountNonce, next) {
		next = accountNonce
	}
	// 这笔落在空号之后，留在那里，不推动可执行号码。
	if txNonce > next {
		return next
	}
	// 从可执行号码往上走。池里有这一笔才加 1，遇到空号就停。替换连续段里的旧交易时，这里不会前进。
	txs := p.bySender[sender]
	for {
		if _, ok := txs[next]; !ok {
			break
		}
		// nonce 已经是最大值，再加会绕回 0。
		if next == ^uint64(0) {
			p.pendingNonce[sender] = next
			return next
		}
		next++
	}
	// 账户 nonce 本身还空着，没有可执行的连续段，不保留记录。
	if next == accountNonce {
		delete(p.pendingNonce, sender)
		return accountNonce
	}
	p.pendingNonce[sender] = next
	return next
}

// filled 报告 [from, to) 这段 nonce 是否每笔都在池里。from >= to 时没有需要检查的号码。
func (p *Pool) filled(sender Address, from, to uint64) bool {
	if from >= to {
		return true
	}
	txs := p.bySender[sender]
	for n := from; n < to; n++ {
		if _, ok := txs[n]; !ok {
			return false
		}
	}
	return true
}

// PendingTxs 按发送方地址、再按 nonce 排序。返回的签名是副本，调用方改不到池里的字节。
func (p *Pool) PendingTxs() []Tx {
	// 排序要同时看发送方和 nonce。map 遍历没有顺序，先摊成带地址的切片。
	type pending struct {
		sender Address
		tx     Tx
	}
	items := make([]pending, 0)
	for sender, txs := range p.bySender {
		for _, tx := range txs {
			items = append(items, pending{sender: sender, tx: tx})
		}
	}
	// 地址按字节序，同一发送方再按 nonce 从小到大。
	sort.Slice(items, func(i, j int) bool {
		if items[i].sender != items[j].sender {
			return bytes.Compare(items[i].sender[:], items[j].sender[:]) < 0
		}
		return items[i].tx.Nonce < items[j].tx.Nonce
	})
	// 返回副本，调用方改 Data 或 Sig 不会改到池里。
	out := make([]Tx, len(items))
	for i, it := range items {
		out[i] = it.tx.Clone()
	}
	return out
}

// DropTx 按发送方和 nonce 丢掉一笔。这个发送方没有别的交易时，外层表也删掉。
func (p *Pool) DropTx(sender Address, nonce uint64) {
	txs := p.bySender[sender]
	if txs == nil {
		return
	}
	delete(txs, nonce)
	if len(txs) == 0 {
		delete(p.bySender, sender)
		delete(p.pendingNonce, sender)
		return
	}
	// 丢掉的号码还在可执行段里时，下一次从这里重新数。空号后面的交易不再推动它。
	if next, ok := p.pendingNonce[sender]; ok && nonce < next {
		p.pendingNonce[sender] = nonce
	}
}

// senderBalanceFits 看余额够不够付这笔，以及从账户 nonce 起连续占住的交易。
// 号码一断开就停，后面的交易不加入这笔总和。同一 nonce 用这笔替换旧的，两笔不一起算。
func (p *Pool) senderBalanceFits(sender Address, tx Tx, balance Uint256, nonce uint64) error {
	sum := Uint256{}
	// 累加前先看会不会超出 2^256-1。费用本身溢出时 txMaxCost 已经返回错误。
	add := func(cost Uint256) error {
		next, overflow := sum.Add(cost)
		if overflow {
			return ErrBalanceOverflow
		}
		sum = next
		return nil
	}
	// 从账户下一笔 nonce 往上走。这个号码在池里没有交易，连续段就到此为止。
	// 发送方还没有内层表时，读出来是空 map，ok 为 false，不会崩溃。
	replaced := false
	for {
		old, ok := p.bySender[sender][nonce]
		if !ok {
			break
		}
		chosen := old
		// 走到这笔自己的 nonce 时，用新费用替换旧的，两笔不一起算。
		if nonce == tx.Nonce {
			chosen = tx
			replaced = true
		}
		cost, err := txMaxCost(chosen)
		if err != nil {
			return err
		}
		if err := add(cost); err != nil {
			return err
		}
		if nonce == ^uint64(0) {
			break
		}
		nonce++
	}
	// 这笔落在空号之后，上面的循环没遇到它。它不和空号后面的其他交易累加，只加到连续段后面。
	if !replaced {
		cost, err := txMaxCost(tx)
		if err != nil {
			return err
		}
		if err := add(cost); err != nil {
			return err
		}
	}
	if sum.Cmp(balance) > 0 {
		return ErrInsufficientBalance
	}
	return nil
}
