package chain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sync"

	"github.com/slmthydesp/minichain/sig"
)

const GenesisBalance uint64 = 1000000

type Chain struct {
	mu        sync.Mutex
	blocks    map[Hash]Block
	states    map[Hash]*State
	canonical Hash
	state     *State
	pool      *Pool
}

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
	block := Block{Height: 0, StateRoot: st.Root()}
	sum := block.Hash()
	return &Chain{
		blocks:    map[Hash]Block{sum: block},
		states:    map[Hash]*State{sum: st},
		canonical: sum,
		state:     st,
		pool:      NewPool(),
	}
}

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

func (c *Chain) Pool() *Pool {
	return c.pool
}

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

func (c *Chain) Mine(coinbase Address) (Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := c.pool.Pending()
	scratch := c.state.Copy()
	chosen := make([]Tx, 0)
	for i := 0; i < len(pending); {
		sender, err := pending[i].Sender()
		if err != nil {
			return Block{}, err
		}
		_, nonce := scratch.BalanceNonce(sender)
		j := i
		for j < len(pending) {
			txSender, err := pending[j].Sender()
			if err != nil {
				return Block{}, err
			}
			if txSender != sender {
				break
			}
			if pending[j].Nonce != nonce {
				break
			}
			if err := Apply(scratch, pending[j]); err != nil {
				break
			}
			chosen = append(chosen, pending[j])
			nonce++
			j++
		}
		for j < len(pending) {
			txSender, err := pending[j].Sender()
			if err != nil {
				return Block{}, err
			}
			if txSender != sender {
				break
			}
			j++
		}
		i = j
	}
	head := c.blocks[c.canonical]
	block := Block{
		Height:     head.Height + 1,
		ParentHash: c.canonical,
		Coinbase:   coinbase,
		Txs:        chosen,
		StateRoot:  scratch.Root(),
	}
	sum := block.Hash()
	c.blocks[sum] = block
	c.states[sum] = scratch
	c.adopt(c.best())
	return block, nil
}

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
	scratch := c.states[b.ParentHash].Copy()
	for _, tx := range b.Txs {
		if err := Apply(scratch, tx); err != nil {
			return err
		}
	}
	if scratch.Root() != b.StateRoot {
		return errors.New("bad state root")
	}
	c.blocks[sum] = b
	c.states[sum] = scratch
	c.adopt(c.best())
	return nil
}

func (c *Chain) best() Hash {
	var best Hash
	var have bool
	for hash, block := range c.blocks {
		if !have {
			best = hash
			have = true
			continue
		}
		cur := c.blocks[best]
		if block.Height > cur.Height || (block.Height == cur.Height && bytes.Compare(hash[:], best[:]) < 0) {
			best = hash
		}
	}
	return best
}

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
