package chain

import (
	"bytes"
	"sort"
)

type pooled struct {
	sender Address
	tx     Tx
}

type Pool struct {
	items []pooled
}

func NewPool() *Pool {
	return &Pool{}
}

func (p *Pool) Add(tx Tx, balance, nonce uint64) error {
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
	for i, it := range p.items {
		if it.sender == sender && it.tx.Nonce == tx.Nonce {
			if tx.GasPrice <= it.tx.GasPrice {
				return ErrReplacementUnderpriced
			}
			stored := tx
			stored.Sig = bytes.Clone(tx.Sig)
			p.items[i] = pooled{sender: sender, tx: stored}
			return nil
		}
	}
	stored := tx
	stored.Sig = bytes.Clone(tx.Sig)
	p.items = append(p.items, pooled{sender: sender, tx: stored})
	return nil
}

func (p *Pool) Pending() []Tx {
	items := append([]pooled(nil), p.items...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].sender != items[j].sender {
			return bytes.Compare(items[i].sender[:], items[j].sender[:]) < 0
		}
		return items[i].tx.Nonce < items[j].tx.Nonce
	})
	out := make([]Tx, len(items))
	for i, it := range items {
		out[i] = it.tx
		out[i].Sig = bytes.Clone(it.tx.Sig)
	}
	return out
}

func (p *Pool) Drop(sender Address, nonce uint64) {
	n := len(p.items)
	kept := p.items[:0]
	for _, it := range p.items {
		if it.sender == sender && it.tx.Nonce == nonce {
			continue
		}
		kept = append(kept, it)
	}
	for i := len(kept); i < n; i++ {
		p.items[i] = pooled{}
	}
	p.items = kept
}
