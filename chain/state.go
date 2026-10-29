package chain

type State struct {
	accounts map[Address]Account
}

func NewState() *State {
	return &State{accounts: make(map[Address]Account)}
}

func (s *State) Copy() *State {
	next := NewState()
	for k, v := range s.accounts {
		next.accounts[k] = v
	}
	return next
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
