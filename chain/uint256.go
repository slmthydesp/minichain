package chain

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/big"
)

// Uint256 是余额和转账金额。大端 32 字节，和 EVM 栈上的字一样宽。
// gas、gasPrice、nonce 仍是 uint64。小于 2^64 的数，RLP 和以前的 8 字节编码相同。
type Uint256 [32]byte

// U256 用一个 uint64 做成 256 位整数。高 24 字节是 0。
func U256(n uint64) Uint256 {
	var u Uint256
	binary.BigEndian.PutUint64(u[24:], n)
	return u
}

// MaxUint256 是 2^256-1。
func MaxUint256() Uint256 {
	var u Uint256
	for i := range u {
		u[i] = 0xff
	}
	return u
}

// maxUint256Minus 是 2^256-1 减去 n。n 来自 uint64，结果不会变负。
func maxUint256Minus(n uint64) Uint256 {
	out, _ := MaxUint256().Sub(U256(n))
	return out
}

// IsZero 表示金额是 0。
func (u Uint256) IsZero() bool {
	return u == Uint256{}
}

// Uint64 在数值放得进 uint64 时返回它。高 24 字节不是 0 时 ok 为 false。
func (u Uint256) Uint64() (uint64, bool) {
	// 前 24 字节必须为 0，后 8 字节存入 uint64 中
	for i := 0; i < 24; i++ {
		if u[i] != 0 {
			return 0, false
		}
	}
	return binary.BigEndian.Uint64(u[24:]), true
}

// Cmp 比较两个金额。u<v 时是 -1，相等是 0，u>v 时是 1。
func (u Uint256) Cmp(v Uint256) int {
	return bytes.Compare(u[:], v[:])
}

// Add 从低字节加起。和超出 2^256-1 时 overflow 为 true，返回值是低 256 位。
//
// 数组是大端：u[0] 最高，u[31] 最低，所以从下标 31 往 0 加。
// 例：0x00ff + 0x0001。先加末字节 ff+01=100，低 8 位 00 写下，进位 1；
// 再加前一字节 00+00+1=01。结果是 0x0100。
func (u Uint256) Add(v Uint256) (Uint256, bool) {
	var out Uint256
	var carry uint16
	for i := 31; i >= 0; i-- {
		// u[i]、v[i] 是 byte。255+255 若仍用 byte 加，会绕成 254，多出的 256 直接丢了。
		// 先转成 uint16，和最大 511，能完整留下，下一行才能拆出进位。
		sum := uint16(u[i]) + uint16(v[i]) + carry
		out[i] = byte(sum) // 低 8 位是这一字节的和
		carry = sum >> 8   // 高 8 位是给更高一字节的进位，只能是 0 或 1
	}
	return out, carry != 0 // 最高字节仍有进位，说明和 ≥ 2^256
}

// Sub 从低字节减起。u<v 时 underflow 为 true。
//
// 数组是大端：u[0] 最高，u[31] 最低，所以从下标 31 往 0 减。
// 例：0x0100 - 0x0001。末字节 00-01 不够，借 1 后写成 ff，并向上借 1；
// 前一字节 01-00-1=00。结果是 0x00ff。
func (u Uint256) Sub(v Uint256) (Uint256, bool) {
	var out Uint256
	var borrow uint16
	for i := 31; i >= 0; i-- {
		// u[i]、v[i] 是 byte。00-01 若仍用 byte 减，会绕成 255，看不出是借来的。
		// 先转成 int，差可以为负，才能判断要不要向更高一字节借 1。
		diff := int(u[i]) - int(v[i]) - int(borrow)
		if diff < 0 {
			diff += 256 // 借来的 1 在这一字节等于 256
			borrow = 1  // 下一轮从更高一字节扣掉
		} else {
			borrow = 0
		}
		out[i] = byte(diff) // 借完后 diff 在 0..255
	}
	return out, borrow != 0 // 最高字节仍要借，说明 u < v
}

// rlp 按最短大端编码这个整数。0 是 0x80。不超过 2^64-1 时和 rlpUint 的字节相同。
func (u Uint256) rlp() []byte {
	if u.IsZero() {
		return []byte{0x80}
	}
	i := 0
	for i < 31 && u[i] == 0 {
		i++
	}
	return rlpString(u[i:])
}

// String 是十进制。
func (u Uint256) String() string {
	return new(big.Int).SetBytes(u[:]).String()
}

// MarshalJSON 由 encoding/json 在 json.Marshal 时调用。
//
// 放得进 uint64 的数写成 JSON 数字，更大的写成十进制字符串：
// json.Marshal(U256(20)) 得到 20；2^64 得到 "18446744073709551616"。
// 结构体里的 Uint256 字段一样，例如交易的 value：
// {"value": 20} 或 {"value": "18446744073709551616"}。
func (u Uint256) MarshalJSON() ([]byte, error) {
	if _, ok := u.Uint64(); ok {
		return []byte(u.String()), nil // 数字字面量，两侧不加引号
	}
	return json.Marshal(u.String()) // 十进制外面加 JSON 引号
}

// UnmarshalJSON 由 encoding/json 在 json.Unmarshal 时调用。
//
// 下面三种都能读进 Uint256：20、"20"、"18446744073709551616"。
// 读结构体时同样，例如 json.Unmarshal([]byte(`{"value":"20"}`), &tx)。
// 负数、空内容、不是十进制、超过 256 位时返回错误。
func (u *Uint256) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		b = []byte(s) // "20" 去掉引号，留下十进制 20
	}
	if len(b) == 0 {
		return errors.New("bad amount")
	}
	n := new(big.Int)
	if _, ok := n.SetString(string(b), 10); !ok || n.Sign() < 0 {
		return errors.New("bad amount")
	}
	if n.BitLen() > 256 {
		return errors.New("amount overflows")
	}
	raw := n.Bytes() // 大端，不含前导 0
	*u = Uint256{}
	copy(u[32-len(raw):], raw) // 高位补 0，数值贴在末尾
	return nil
}
