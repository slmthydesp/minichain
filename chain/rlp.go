package chain

import "encoding/binary"

// 资料：https://www.cnblogs.com/zhanchenjin/p/18920415

// 这里只做 RLP 编码，不解码。状态账户、回执、合约地址和 Merkle Patricia Trie 的节点都靠它得到稳定字节。
// 用到的只有字符串、非负整数和列表。载荷不超过 55 字节时，一个字节同时写下类型和长度；更长时，先写下长度本身占几字节。

// rlpString 按以太坊 RLP 编码一段字节。单字节且小于 0x80 时不加重头，避免和长度头混在一起。
func rlpString(b []byte) []byte {
	if len(b) == 1 && b[0] < 0x80 {
		return []byte{b[0]}
	}
	return rlpPrefix(0x80, b)
}

// rlpUint 按以太坊 RLP 编码非负整数。0 编码成空字节串 0x80。其余去掉前导零，再按最短的大端字节编码。
func rlpUint(n uint64) []byte {
	if n == 0 {
		return []byte{0x80}
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], n)
	i := 0
	for buf[i] == 0 {
		i++
	}
	return rlpString(buf[i:])
}

// rlpList 把已经各自编码好的项收成一个 RLP 列表。
func rlpList(items ...[]byte) []byte {
	n := 0
	for _, item := range items {
		n += len(item)
	}
	payload := make([]byte, 0, n)
	for _, item := range items {
		payload = append(payload, item...)
	}
	return rlpPrefix(0xc0, payload)
}

// rlpPrefix 给一段已经拼好的载荷加上类型和长度头。offset 是 0x80（字符串）或 0xc0（列表）。
// 载荷不超过 55 字节时，头是 offset 加上长度。更长时，头是 offset+55 再加上长度所占的字节数，后面紧跟长度的大端字节。
func rlpPrefix(offset byte, payload []byte) []byte {
	if len(payload) <= 55 {
		out := make([]byte, 1+len(payload))
		out[0] = offset + byte(len(payload))
		copy(out[1:], payload)
		return out
	}
	var lenBuf [8]byte
	binary.BigEndian.PutUint64(lenBuf[:], uint64(len(payload)))
	i := 0
	for lenBuf[i] == 0 {
		i++
	}
	size := lenBuf[i:]
	out := make([]byte, 1+len(size)+len(payload))
	out[0] = offset + 55 + byte(len(size))
	copy(out[1:], size)
	copy(out[1+len(size):], payload)
	return out
}
