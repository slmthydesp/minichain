package chain

import (
	"bytes"
	"sort"
)

// Pool 是内存里的交易池。同一发送方可以先挂上未来的 nonce，出块时再按连续 nonce 取出。
// bySender 的外层键是发送方，内层键是交易的 nonce。同一 nonce 只有一笔。
// 需要顺序时由 Pending 函数另做一份，按地址再按 nonce 排序。
type Pool struct {
	bySender map[Address]map[uint64]Tx
}

func NewPool() *Pool {
	return &Pool{bySender: make(map[Address]map[uint64]Tx)}
}

// Add 把交易放进池。「balance 和 nonce 是发送方在当前链头上的账户状态」，不是交易里的 tx.Nonce。
// nonce 是这个账户已经入块的交易数。tx.Nonce 比它小说明这笔已经用过，拒绝。
// tx.Nonce 比它大可以先挂在池里，等前面的号码入块后再执行。
// 同一发送方已经在池里的其他交易也要占用余额。
// 同一发送方、同一 tx.Nonce 已经在池里时，只有 gasPrice 严格更高才替换原交易。
func (p *Pool) Add(tx Tx, balance, nonce uint64) error {
	// 参数 balance 和 nonce 就是当前用户链头的最新余额和nonce值
	sender, err := tx.Sender()
	if err != nil {
		return ErrBadSignature
	}
	if err := checkTx(tx, balance); err != nil {
		return err
	}
	if tx.Nonce < nonce {
		return ErrNonceTooLow
	}
	if err := p.fitsWithPending(sender, tx, balance); err != nil {
		return err
	}
	txs := p.bySender[sender]
	if old, ok := txs[tx.Nonce]; ok {
		if tx.GasPrice <= old.GasPrice {
			return ErrReplacementUnderpriced
		}
	}
	p.store(sender, tx)
	return nil
}

// store 放进这个发送方的 nonce 槽。签名单独拷一份，调用方改自己的字节不会改到池里。
func (p *Pool) store(sender Address, tx Tx) {
	txs := p.bySender[sender]
	if txs == nil {
		txs = make(map[uint64]Tx)
		p.bySender[sender] = txs
	}
	stored := tx
	stored.Sig = bytes.Clone(tx.Sig)
	txs[tx.Nonce] = stored
}

// NextNonce 是这个发送方下一个还没被池占住的 nonce。accountNonce 是链头上已经入块的值。
// 池里有缺口时，先填缺口，不跳到更高的号码。
func (p *Pool) NextNonce(sender Address, accountNonce uint64) uint64 {
	txs := p.bySender[sender]
	next := accountNonce
	for {
		if _, ok := txs[next]; !ok {
			return next
		}
		if next == ^uint64(0) {
			return next
		}
		next++
	}
}

// Pending 按发送方地址、再按 nonce 排序。返回的签名是副本，调用方改不到池里的字节。
func (p *Pool) Pending() []Tx {
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
	// Tx 是值拷贝，但 Sig 是切片，和池里共享底层数组，所以单独拷一份。
	out := make([]Tx, len(items))
	for i, it := range items {
		out[i] = it.tx
		out[i].Sig = bytes.Clone(it.tx.Sig)
	}
	return out
}

// Drop 按发送方和 nonce 丢掉一笔。这个发送方没有别的交易时，外层表也删掉。
func (p *Pool) Drop(sender Address, nonce uint64) {
	txs := p.bySender[sender]
	if txs == nil {
		return
	}
	delete(txs, nonce)
	if len(txs) == 0 {
		delete(p.bySender, sender)
	}
}

// fitsWithPending 把这笔交易和同一发送方的其他待处理交易加在一起，看余额够不够。
// 正在被替换的那一笔（相同 nonce）不重复计算。
func (p *Pool) fitsWithPending(sender Address, tx Tx, balance uint64) error {
	sum, err := txCost(tx)
	if err != nil {
		return err
	}
	for _, it := range p.bySender[sender] {
		if it.Nonce == tx.Nonce {
			continue
		}
		cost, err := txCost(it)
		if err != nil {
			return err
		}
		if sum > ^uint64(0)-cost {
			return ErrBalanceOverflow
		}
		sum += cost
	}
	if sum > balance {
		return ErrInsufficientBalance
	}
	return nil
}
