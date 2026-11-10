package chain

import "github.com/slmthydesp/minichain/sig"

// 预编译放在固定地址上，由客户端用本地代码执行，不解释账户里的字节码。
// 目前只有 ecrecover。CALL、CALLCODE、DELEGATECALL、STATICCALL 和交易入口打到这个地址都走这里。

const (
	// gasEcrecover 是 ecrecover 固定收取的 gas。签名合不合法都收，gas 不够则这次调用失败。
	gasEcrecover uint64 = 3000
	// ecrecoverInputLen 是 (hash、v、r、s) 各 32 字节。短了的输入右侧补 0，长了的只看前 128 字节。
	ecrecoverInputLen = 128
)

// precompileEcrecover 是 ecrecover 的地址，20 字节里只有最后一字节是 1。
// 0x0000000000000000000000000000000000000001
var precompileEcrecover = Address{19: 1}

// runPrecompile 在 addr 是预编译时执行它。ok 为 false 表示这个地址不在表里，调用方按普通账户继续。
// 签名不合法时 ret 为空、kind 为 execOK：调用成功，但不给出地址，也不把外层打成回退。
// 转给它的 gas 少于 3000 时 kind 为 execHalt，剩余 gas 记 0。调用方据此丢掉这次附带的转账。
func runPrecompile(addr Address, input []byte, gas uint64) (ret []byte, left uint64, kind execKind, ok bool) {
	if addr != precompileEcrecover {
		return nil, 0, 0, false
	}
	if gas < gasEcrecover {
		return nil, 0, execHalt, true
	}
	return ecrecover(input), gas - gasEcrecover, execOK, true
}

// ecrecover 从 128 字节输入恢复地址。布局是 (hash、v、r、s)，各 32 字节。
// v 这一字只有最后一字节可以是 27 或 28，前面 31 字节必须是 0。r、s 要落在 1 到曲线阶减 1。
// 任何一项不合法都返回 nil。返回值是 32 字节，地址放在末尾 20 字节。
func ecrecover(input []byte) []byte {
	buf := make([]byte, ecrecoverInputLen)
	if len(input) > ecrecoverInputLen {
		input = input[:ecrecoverInputLen]
	}
	// hash [0:32)
	// v [32:64)
	// r [64:96)
	// s [96:128)
	copy(buf, input)
	// v 的前 31 字节必须全是 0, 最后 1 字节只能是 27 或 28
	for _, b := range buf[32:63] {
		if b != 0 {
			return nil
		}
	}
	v := buf[63]
	if v != 27 && v != 28 {
		return nil
	}

	// 拼接 sigBytes，65 字节：v ++ r ++ s
	sigBytes := make([]byte, 65)
	sigBytes[0] = v
	copy(sigBytes[1:33], buf[64:96])
	copy(sigBytes[33:], buf[96:128])
	addr, err := sig.Recover(buf[:32], sigBytes)
	if err != nil {
		return nil
	}
	out := make([]byte, 32)
	copy(out[12:], addr[:])
	return out
}
