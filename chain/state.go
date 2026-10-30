package chain

import (
	"bytes"
	"encoding/binary"
	"sort"
)

type State struct {
	accounts map[Address]Account
	coinbase Address
}

func NewState() *State {
	return &State{accounts: make(map[Address]Account)}
}

func (s *State) Copy() *State {
	next := NewState()
	next.coinbase = s.coinbase
	for k, v := range s.accounts {
		next.accounts[k] = v
	}
	return next
}

func (s *State) SetCoinbase(a Address) {
	s.coinbase = a
}

func (s *State) Get(a Address) (Account, bool) {
	acct, ok := s.accounts[a]
	return acct, ok
}

func (s *State) Put(a Account) {
	s.accounts[a.Address] = a
}

func (s *State) BalanceNonce(a Address) (uint64, uint64) {
	acct, ok := s.accounts[a]
	if !ok {
		return 0, 0
	}
	return acct.Balance, acct.Nonce
}

func (s *State) Delete(a Address) {
	delete(s.accounts, a)
}

func (s *State) Root() Hash {
	addrs := make([]Address, 0, len(s.accounts))
	for addr := range s.accounts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return bytes.Compare(addrs[i][:], addrs[j][:]) < 0
	})
	buf := make([]byte, 0, len(addrs)*36)
	var n [8]byte
	for _, addr := range addrs {
		acct := s.accounts[addr]
		buf = append(buf, acct.Address[:]...)
		binary.BigEndian.PutUint64(n[:], acct.Balance)
		buf = append(buf, n[:]...)
		binary.BigEndian.PutUint64(n[:], acct.Nonce)
		buf = append(buf, n[:]...)
	}
	return keccak(buf)
}
