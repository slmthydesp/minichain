package chain

import "errors"

var (
	// ErrBadSignature 表示签名恢复不出发送方。
	ErrBadSignature = errors.New("bad signature")
	// ErrWrongChainID 表示交易的 chain id 不是 ChainID。
	ErrWrongChainID = errors.New("wrong chain id")
	// ErrIntrinsicGas 表示 gasLimit 小于这笔交易的起步 gas。
	ErrIntrinsicGas = errors.New("intrinsic gas")
	// ErrNonceTooLow 表示交易 nonce 低于账户已经用过的 nonce，池不会收下。
	ErrNonceTooLow = errors.New("nonce too low")
	// ErrInsufficientBalance 表示余额不够支付 value 加上按 gasLimit 预留的最高费用。
	ErrInsufficientBalance = errors.New("insufficient balance")
	// ErrNonceUsed 保留给已经占住的 nonce。池里的同 nonce 现在改走单价替换。
	ErrNonceUsed = errors.New("nonce used")
	// ErrReplacementUnderpriced 表示同 nonce 的新交易单价没有严格高于池里的那笔。
	ErrReplacementUnderpriced = errors.New("replacement underpriced")
	// ErrNonceMismatch 表示执行时 nonce 不等于账户当前 nonce。
	ErrNonceMismatch = errors.New("nonce mismatch")
	// ErrBalanceOverflow 表示费用乘法或入账会超出 uint64。
	ErrBalanceOverflow = errors.New("balance overflow")
)
