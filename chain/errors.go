package chain

import "errors"

var (
	ErrBadSignature        = errors.New("bad signature")
	ErrWrongChainID        = errors.New("wrong chain id")
	ErrNonceTooLow         = errors.New("nonce too low")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrNonceUsed           = errors.New("nonce used")
	ErrNonceMismatch       = errors.New("nonce mismatch")
	ErrBalanceOverflow     = errors.New("balance overflow")
)
