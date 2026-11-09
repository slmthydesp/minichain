package chain

import (
	"math/big"
	"math/bits"
)

// 字运算都在 2^256 上取模。binop 先弹出的 a 是栈顶。
// 除数或模数为 0 时，结果是 0。有符号运算把最高位当作符号位。

func mulWord(a, b Hash) Hash {
	x := new(big.Int).SetBytes(a[:])
	y := new(big.Int).SetBytes(b[:])
	return hashMod(x.Mul(x, y))
}

func divWord(a, b Hash) Hash {
	if b == (Hash{}) {
		return Hash{}
	}
	x := new(big.Int).SetBytes(a[:])
	y := new(big.Int).SetBytes(b[:])
	return hashMod(x.Div(x, y))
}

func sdivWord(a, b Hash) Hash {
	if b == (Hash{}) {
		return Hash{}
	}
	x := wordSigned(a)
	y := wordSigned(b)
	return hashMod(x.Quo(x, y))
}

func modWord(a, b Hash) Hash {
	if b == (Hash{}) {
		return Hash{}
	}
	x := new(big.Int).SetBytes(a[:])
	y := new(big.Int).SetBytes(b[:])
	return hashMod(x.Mod(x, y))
}

func smodWord(a, b Hash) Hash {
	if b == (Hash{}) {
		return Hash{}
	}
	x := wordSigned(a)
	y := wordSigned(b)
	// Rem 的符号跟被除数走，和 EVM 的向零截断一致。
	return hashMod(x.Rem(x, y))
}

func addModWord(a, b, n Hash) Hash {
	if n == (Hash{}) {
		return Hash{}
	}
	x := new(big.Int).SetBytes(a[:])
	y := new(big.Int).SetBytes(b[:])
	m := new(big.Int).SetBytes(n[:])
	x.Add(x, y)
	return hashMod(x.Mod(x, m))
}

func mulModWord(a, b, n Hash) Hash {
	if n == (Hash{}) {
		return Hash{}
	}
	x := new(big.Int).SetBytes(a[:])
	y := new(big.Int).SetBytes(b[:])
	m := new(big.Int).SetBytes(n[:])
	x.Mul(x, y)
	return hashMod(x.Mod(x, m))
}

func expWord(base, exp Hash) Hash {
	x := new(big.Int).SetBytes(base[:])
	e := new(big.Int).SetBytes(exp[:])
	mod := new(big.Int).Lsh(big.NewInt(1), 256)
	return hashMod(x.Exp(x, e, mod))
}

// signExtendWord 按 back 指定的字节做符号扩展。back 从最低字节数起，大于 31 时原样返回。
func signExtendWord(back, num Hash) Hash {
	n, ok := hashUint64(back)
	if !ok || n > 31 {
		return num
	}
	out := num
	signByte := out[31-int(n)]
	fill := byte(0)
	if signByte&0x80 != 0 {
		fill = 0xff
	}
	for i := 0; i < 31-int(n); i++ {
		out[i] = fill
	}
	return out
}

func gtWord(a, b Hash) Hash {
	if ltWord(b, a) == hashFromUint64(1) {
		return hashFromUint64(1)
	}
	return Hash{}
}

func sltWord(a, b Hash) Hash {
	if wordSigned(a).Cmp(wordSigned(b)) < 0 {
		return hashFromUint64(1)
	}
	return Hash{}
}

func sgtWord(a, b Hash) Hash {
	if wordSigned(a).Cmp(wordSigned(b)) > 0 {
		return hashFromUint64(1)
	}
	return Hash{}
}

func andWord(a, b Hash) Hash {
	var out Hash
	for i := range out {
		out[i] = a[i] & b[i]
	}
	return out
}

func orWord(a, b Hash) Hash {
	var out Hash
	for i := range out {
		out[i] = a[i] | b[i]
	}
	return out
}

func xorWord(a, b Hash) Hash {
	var out Hash
	for i := range out {
		out[i] = a[i] ^ b[i]
	}
	return out
}

func notWord(a Hash) Hash {
	var out Hash
	for i := range out {
		out[i] = ^a[i]
	}
	return out
}

// byteWord 取 val 从最高字节数起的第 idx 个字节。idx 不小于 32 时是 0。
func byteWord(idx, val Hash) Hash {
	n, ok := hashUint64(idx)
	if !ok || n >= 32 {
		return Hash{}
	}
	return hashFromUint64(uint64(val[n]))
}

func shlWord(shift, val Hash) Hash {
	n, ok := hashUint64(shift)
	if !ok || n >= 256 {
		return Hash{}
	}
	x := new(big.Int).SetBytes(val[:])
	return hashMod(x.Lsh(x, uint(n)))
}

func shrWord(shift, val Hash) Hash {
	n, ok := hashUint64(shift)
	if !ok || n >= 256 {
		return Hash{}
	}
	x := new(big.Int).SetBytes(val[:])
	return hashMod(x.Rsh(x, uint(n)))
}

func sarWord(shift, val Hash) Hash {
	n, ok := hashUint64(shift)
	if !ok || n >= 256 {
		if val[0]&0x80 != 0 {
			return hashOnes()
		}
		return Hash{}
	}
	x := wordSigned(val)
	return hashMod(x.Rsh(x, uint(n)))
}

func clzWord(h Hash) Hash {
	for i, b := range h {
		if b != 0 {
			return hashFromUint64(uint64(i*8 + bits.LeadingZeros8(b)))
		}
	}
	return hashFromUint64(256)
}

func wordSigned(h Hash) *big.Int {
	x := new(big.Int).SetBytes(h[:])
	if h[0]&0x80 != 0 {
		mod := new(big.Int).Lsh(big.NewInt(1), 256)
		x.Sub(x, mod)
	}
	return x
}

func hashOnes() Hash {
	var h Hash
	for i := range h {
		h[i] = 0xff
	}
	return h
}
