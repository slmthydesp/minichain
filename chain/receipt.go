package chain

import "encoding/binary"

// Log 是执行成功时记下的一条日志。回退的交易不保留日志。
// Address 是正在执行 LOG 的合约地址，不是发起这笔交易的外部账户。
type Log struct {
	Address Address
	Topics  []Hash
	Data    []byte
}

// Receipt 是一笔已经入块的交易的执行结果。回退或 gas 用尽时 Status 是 0，成功是 1。
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

// ReceiptsRoot 是回执的 Merkle Patricia Trie 根。键是序号的 RLP。
// 值是 status、gasUsed、cumulativeGasUsed 和日志的 RLP 列表。空列表的根是空树根。
func ReceiptsRoot(rs []Receipt) Hash {
	if len(rs) == 0 {
		return emptyTrieRoot()
	}
	keys := make([][]byte, len(rs))
	vals := make([][]byte, len(rs))
	for i, r := range rs {
		keys[i] = rlpUint(uint64(i))
		vals[i] = r.rlp()
	}
	return trieRoot(keys, vals)
}

func (r Receipt) rlp() []byte {
	logs := make([][]byte, len(r.Logs))
	for i, lg := range r.Logs {
		topics := make([][]byte, len(lg.Topics))
		for j, topic := range lg.Topics {
			topics[j] = rlpString(topic[:])
		}
		logs[i] = rlpList(rlpString(lg.Address[:]), rlpList(topics...), rlpString(lg.Data))
	}
	return rlpList(rlpUint(r.Status), rlpUint(r.GasUsed), rlpUint(r.CumulativeGasUsed), rlpList(logs...))
}
