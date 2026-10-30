package chain

import (
	"errors"
	"sync"

	"github.com/slmthydesp/minichain/sig"
)

const GenesisBalance uint64 = 1000000

type Chain struct {
	mu     sync.Mutex
	blocks []Block
	state  *State
	pool   *Pool
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
	return &Chain{
		blocks: []Block{block},
		state:  st,
		pool:   NewPool(),
	}
}

func (c *Chain) Head() Block {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.blocks[len(c.blocks)-1]
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
	out := []Block{}
	for _, block := range c.blocks {
		if block.Height > height {
			out = append(out, block)
		}
	}
	return out
}

func (c *Chain) Mine() (Block, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := c.pool.Pending()
	scratch := c.state.Copy()
	type mined struct {
		sender Address
		tx     Tx
	}
	chosen := make([]mined, 0)
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
			chosen = append(chosen, mined{sender: txSender, tx: pending[j]})
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
	txs := make([]Tx, len(chosen))
	for i, item := range chosen {
		c.pool.Drop(item.sender, item.tx.Nonce)
		txs[i] = item.tx
	}
	head := c.blocks[len(c.blocks)-1]
	block := Block{
		Height:     head.Height + 1,
		ParentHash: head.Hash(),
		Txs:        txs,
		StateRoot:  scratch.Root(),
	}
	c.blocks = append(c.blocks, block)
	c.state = scratch
	return block, nil
}

func (c *Chain) Append(b Block) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	head := c.blocks[len(c.blocks)-1]
	if b.ParentHash != head.Hash() {
		return errors.New("bad parent hash")
	}
	if b.Height != head.Height+1 {
		return errors.New("bad height")
	}
	scratch := c.state.Copy()
	senders := make([]Address, len(b.Txs))
	for i, tx := range b.Txs {
		sender, err := tx.Sender()
		if err != nil {
			return err
		}
		if err := Apply(scratch, tx); err != nil {
			return err
		}
		senders[i] = sender
	}
	if scratch.Root() != b.StateRoot {
		return errors.New("bad state root")
	}
	c.blocks = append(c.blocks, b)
	c.state = scratch
	for i, tx := range b.Txs {
		c.pool.Drop(senders[i], tx.Nonce)
	}
	return nil
}
