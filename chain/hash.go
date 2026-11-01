package chain

import "github.com/slmthydesp/minichain/sig"

// keccak 计算 Keccak-256，并收成链上使用的 32 字节 Hash 类型。
func keccak(data []byte) Hash {
	sum := sig.Keccak(data)
	var out Hash
	copy(out[:], sum[:])
	return out
}
