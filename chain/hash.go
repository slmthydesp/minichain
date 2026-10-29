package chain

import "github.com/slmthydesp/minichain/sig"

func keccak(data []byte) Hash {
	sum := sig.Keccak(data)
	var out Hash
	copy(out[:], sum[:])
	return out
}
