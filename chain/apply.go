package chain

const (
	IntrinsicGas  uint64 = 21000
	BlockGasLimit uint64 = 100000
)

func gasProduct(gas, price uint64) (uint64, error) {
	if price != 0 && gas > ^uint64(0)/price {
		return 0, ErrBalanceOverflow
	}
	return gas * price, nil
}

func checkTx(tx Tx, balance uint64) error {
	if tx.ChainID != ChainID {
		return ErrWrongChainID
	}
	if tx.GasLimit < IntrinsicGas {
		return ErrIntrinsicGas
	}
	maxFee, err := gasProduct(tx.GasLimit, tx.GasPrice)
	if err != nil {
		return err
	}
	if tx.Value > ^uint64(0)-maxFee {
		return ErrBalanceOverflow
	}
	if tx.Value+maxFee > balance {
		return ErrInsufficientBalance
	}
	return nil
}

func Apply(s *State, tx Tx) (uint64, error) {
	sender, err := tx.Sender()
	if err != nil {
		return 0, ErrBadSignature
	}
	from, ok := s.Get(sender)
	if !ok {
		from = Account{Address: sender}
	}
	if err := checkTx(tx, from.Balance); err != nil {
		return 0, err
	}
	if tx.Nonce != from.Nonce {
		return 0, ErrNonceMismatch
	}
	fee, err := gasProduct(IntrinsicGas, tx.GasPrice)
	if err != nil {
		return 0, err
	}

	coinbase := s.coinbase
	fromAcct := from
	toAcct := Account{Address: tx.To}
	if sender != tx.To {
		if acct, ok := s.Get(tx.To); ok {
			toAcct = acct
		}
	}
	coinAcct := Account{Address: coinbase}
	distinctCoinbase := coinbase != sender && coinbase != tx.To
	if distinctCoinbase {
		if acct, ok := s.Get(coinbase); ok {
			coinAcct = acct
		}
	}

	if sender == tx.To {
		fromAcct.Balance -= fee
	} else {
		fromAcct.Balance -= tx.Value + fee
		if toAcct.Balance > ^uint64(0)-tx.Value {
			return 0, ErrBalanceOverflow
		}
		toAcct.Balance += tx.Value
	}
	fromAcct.Nonce++

	switch {
	case coinbase == sender:
		if fromAcct.Balance > ^uint64(0)-fee {
			return 0, ErrBalanceOverflow
		}
		fromAcct.Balance += fee
	case coinbase == tx.To:
		if toAcct.Balance > ^uint64(0)-fee {
			return 0, ErrBalanceOverflow
		}
		toAcct.Balance += fee
	default:
		if coinAcct.Balance > ^uint64(0)-fee {
			return 0, ErrBalanceOverflow
		}
		coinAcct.Balance += fee
	}

	s.Put(fromAcct)
	if sender != tx.To {
		s.Put(toAcct)
	}
	if distinctCoinbase {
		s.Put(coinAcct)
	}
	return IntrinsicGas, nil
}
