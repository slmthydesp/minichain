package chain

import "errors"

var (
	ErrBadSignature        = errors.New("bad signature")
	ErrNonceTooLow         = errors.New("nonce too low")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrNonceUsed           = errors.New("nonce used")
)
