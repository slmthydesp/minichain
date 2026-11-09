package chain

import "encoding/binary"

// 资料：https://www.cnblogs.com/zhanchenjin/p/18920415

// 这里只做 RLP 编码，不解码。状态账户、回执、合约地址和 Merkle Patricia Trie 的节点都靠它得到稳定字节。
// 用到的只有字符串、非负整数和列表。字符串和列表都按载荷是否超过 55 字节分成短、长两种。
//
// 已经包括的情况：
//   - 单字节：正好 1 字节且小于 0x80，原样写出，不加头。
//   - 短字符串：0 到 55 字节。头是 0x80+长度，空串就是 0x80 长度是 0，范围 0x80 ～ 0xb7。
//   - 长字符串：超过 55 字节。头从 0xb8 起，先写下长度占几字节，再写下长度。56 字节的长度占 1 字节，256 字节占 2 字节。
//   - 非负整数：0 编成空串 0x80。其余去掉前导零，再按上面的字符串规则编码。uint64 最大值是 8 字节，仍落在短字符串。
//   - 短列表：各项拼起来 0 到 55 字节。头是 0xc0 加长度，空列表就是 0xc0。
//   - 长列表：各项拼起来超过 55 字节。头从 0xf8 起，同样先写下长度占几字节。多项短字符串加起来超过 55，或里面嵌一条长字符串，都走这里。

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
