package chain

import "encoding/binary"

// Log 是执行成功时记下的一条日志。回退的交易不保留日志。
// Address 是正在执行 LOG 的合约地址，不是发起这笔交易的外部账户。
type Log struct {
	Address Address
	// Topics 每项是 [32]byte。LOG 从栈上弹出 topic，栈上每个字固定 32 字节；任意长度的内容放在 Data。
	Topics []Hash
	Data   []byte
}

// Receipt 是一笔已经入块的交易的执行结果。回退或停机时 Status 是 0，成功是 1。
// GasUsed 是这笔实际消耗的 gas，CumulativeGasUsed 是本块到这笔为止的累计值。
type Receipt struct {
	Status            uint64
	GasUsed           uint64
	CumulativeGasUsed uint64
	Logs              []Log
}

// Bytes 把三个 uint64 编成 24 字节大端。
func (r Receipt) Bytes() []byte {
	buf := make([]byte, 24)
	binary.BigEndian.PutUint64(buf[0:8], r.Status)
	binary.BigEndian.PutUint64(buf[8:16], r.GasUsed)
	binary.BigEndian.PutUint64(buf[16:24], r.CumulativeGasUsed)
	return buf
}

// ReceiptsRoot 是回执的 Merkle Patricia Trie 根。
// 键是这笔回执在本块中的序号，从 0 起，和区块体里对应交易的位置相同，再做 RLP。
// 值是 status、gasUsed、cumulativeGasUsed 和日志的 RLP 列表。空列表的根是空树根。
// 1. 区块头里只留这个根
// 2. 回执原文另外放在区块里
// 3. 别人拿到原文，用同样的键和值再算一次根，对得上就说明这批回执没有被改过。对不上就拒绝这个块
func ReceiptsRoot(rs []Receipt) Hash {
	if len(rs) == 0 {
		return emptyTrieRoot()
	}
	keys := make([][]byte, len(rs))
	vals := make([][]byte, len(rs))
	for i, r := range rs {
		keys[i] = rlpUint(uint64(i)) // key
		vals[i] = r.rlp()            // value，执行结果
	}
	// 建一棵 Merkle Patricia Trie，键是序号的 RLP，值是回执 Receipt 的 RLP 编码
	return trieRoot(keys, vals)
}

// rlp 是回执树里这条记录的值。
// 外层是 [status, gasUsed, cumulativeGasUsed, logs]。
// 每条日志是 [address, topics, data]：地址、每个 topic、data 都是字符串，topics 再包成列表。
func (r Receipt) rlp() []byte {
	logs := make([][]byte, len(r.Logs))
	for i, lg := range r.Logs {
		topics := make([][]byte, len(lg.Topics))
		for j, topic := range lg.Topics {
			topics[j] = rlpString(topic[:]) // 每个 topic 固定 32 字节
		}
		logs[i] = rlpList(rlpString(lg.Address[:]), rlpList(topics...), rlpString(lg.Data))
	}
	return rlpList(rlpUint(r.Status), rlpUint(r.GasUsed), rlpUint(r.CumulativeGasUsed), rlpList(logs...))
}
