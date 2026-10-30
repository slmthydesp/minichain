package chain

import "encoding/binary"

type Receipt struct {
	Status            uint64
	GasUsed           uint64
	CumulativeGasUsed uint64
}

func (r Receipt) Bytes() []byte {
	buf := make([]byte, 24)
	binary.BigEndian.PutUint64(buf[0:8], r.Status)
	binary.BigEndian.PutUint64(buf[8:16], r.GasUsed)
	binary.BigEndian.PutUint64(buf[16:24], r.CumulativeGasUsed)
	return buf
}

func ReceiptsRoot(rs []Receipt) Hash {
	buf := make([]byte, 0, len(rs)*24)
	for _, r := range rs {
		buf = append(buf, r.Bytes()...)
	}
	return keccak(buf)
}
