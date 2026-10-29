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
	if tx.Nonce < nonce {
		return ErrNonceTooLow
	}
	if tx.Value > balance {
		return ErrInsufficientBalance
	}
	for _, it := range p.items {
		if it.sender == sender && it.tx.Nonce == tx.Nonce {
			return ErrNonceUsed
		}
	}
	p.items = append(p.items, pooled{sender: sender, tx: tx})
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
	}
	return out
}

func (p *Pool) Drop(sender Address, nonce uint64) {
	kept := p.items[:0]
	for _, it := range p.items {
		if it.sender == sender && it.tx.Nonce == nonce {
			continue
		}
		kept = append(kept, it)
	}
	p.items = kept
}
