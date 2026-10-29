package chain

func Apply(s *State, tx Tx) error {
	sender, err := tx.Sender()
	if err != nil {
		return ErrBadSignature
	}
	from, ok := s.Get(sender)
	if !ok {
		from = Account{Address: sender}
	}
	if tx.Nonce != from.Nonce {
		return ErrNonceMismatch
	}
	if tx.Value > from.Balance {
		return ErrInsufficientBalance
	}
	if tx.To == sender {
		from.Nonce++
		s.Put(from)
		return nil
	}
	to, ok := s.Get(tx.To)
	if !ok {
		to = Account{Address: tx.To}
	}
	if to.Balance > ^uint64(0)-tx.Value {
		return ErrBalanceOverflow
	}
	from.Nonce++
	from.Balance -= tx.Value
	to.Balance += tx.Value
	s.Put(from)
	s.Put(to)
	return nil
}
