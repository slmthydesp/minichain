package chain

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

// 这个文件测地址 0x01 的 ecrecover。
// 签名不合法时返回空，调用仍然成功。gas 不够 3000 才让这次调用失败。

// ecrecoverInput 把 32 字节摘要和 65 字节紧凑签名排成预编译的 128 字节输入。
// 紧凑签名是 v、r、s。v 放在第二个字的最后一字节，r 和 s 各占一个字。
func ecrecoverInput(hash, signature []byte) []byte {
	in := make([]byte, ecrecoverInputLen)
	copy(in, hash)                     // [0:32) 摘要
	in[63] = signature[0]              // [32:64) 的最后一字节是 v
	copy(in[64:96], signature[1:33])   // [64:96) r
	copy(in[96:128], signature[33:65]) // [96:128) s
	return in
}

// demoSignedHash 用演示私钥对 "ecrecover" 的 Keccak 签名。
// 返回摘要、65 字节紧凑签名，以及这把私钥对应的地址。
func demoSignedHash(t *testing.T) (hash [32]byte, signature []byte, addr Address) {
	t.Helper()
	key := demoKey(t)
	hash = sig.Keccak([]byte("ecrecover"))
	var err error
	signature, err = sig.Sign(key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sig.KeyAddress(key)
	if err != nil {
		t.Fatal(err)
	}
	copy(addr[:], raw[:])
	return hash, signature, addr
}

// 合法签名恢复出签名者，地址在返回值末尾 20 字节。
// 把 s 换成曲线阶减 s，并把恢复标记的奇偶对调，恢复出的仍是同一地址。
func TestEcrecoverAcceptsCompactSignature(t *testing.T) {
	hash, signature, addr := demoSignedHash(t)
	want := hashFromAddress(addr)
	got := ecrecover(ecrecoverInput(hash[:], signature))
	if !bytes.Equal(got, want[:]) {
		t.Fatalf("recovered = %x", got)
	}

	// s 换成 N-s，恢复标记在 27 和 28 之间对调，恢复出的仍是同一地址。
	n := new(big.Int)
	n.SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16)
	s := new(big.Int).SetBytes(signature[33:])
	s.Sub(n, s)
	high := bytes.Clone(signature)
	// 紧凑签名的恢复标记是 27 加上 0 到 3。对调奇偶位才是 27 和 28 互换，不是对整个字节异或 1。
	high[0] = 27 + ((signature[0] - 27) ^ 1)
	copy(high[33:], s.FillBytes(make([]byte, 32)))
	if got := ecrecover(ecrecoverInput(hash[:], high)); !bytes.Equal(got, want[:]) {
		t.Fatalf("high s = %x", got)
	}
}

// 改坏 r、v 的高位不是 0、输入短到没有 v、r 等于曲线阶，都返回空，不报错。
// gas 比 3000 少 1 时这次调用停机，剩余 gas 是 0。
// 签名不合法但 gas 够时调用成功，只扣 3000，多给的 8 退回。地址 0x02 不在预编译表里。
func TestEcrecoverRejectsBadInputWithoutError(t *testing.T) {
	hash, signature, _ := demoSignedHash(t)
	input := ecrecoverInput(hash[:], signature)
	flipped := bytes.Clone(input)
	// 第 80 字节落在 r 里。翻掉它之后签名对不上。
	flipped[80] ^= 0x01
	if ecrecover(flipped) != nil {
		t.Fatal("expected flipped signature to return empty")
	}
	// v 字的高位不是 0。最后一字节仍是 27 或 28 也不接受。
	wide := bytes.Clone(input)
	wide[32] = 1
	if ecrecover(wide) != nil {
		t.Fatal("expected wide v to return empty")
	}
	// 只留下摘要。v、r、s 读出来是 0，v 不是 27 或 28。
	short := input[:32]
	if ecrecover(short) != nil {
		t.Fatal("expected short input to return empty")
	}
	// 曲线阶本身不能当 r。
	order := bytes.Clone(input)
	n := new(big.Int)
	n.SetString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141", 16)
	copy(order[64:96], n.FillBytes(make([]byte, 32)))
	if ecrecover(order) != nil {
		t.Fatal("expected r = N to return empty")
	}
	// gas 不够时这次调用失败。签名不合法但仍够 3000 时，调用成功，返回空，gas 只扣 3000。
	if _, left, kind, ok := runPrecompile(precompileEcrecover, input, gasEcrecover-1); !ok || kind != execHalt || left != 0 {
		t.Fatalf("out of gas kind=%d left=%d ok=%v", kind, left, ok)
	}
	ret, left, kind, ok := runPrecompile(precompileEcrecover, flipped, gasEcrecover+8)
	if !ok || kind != execOK || left != 8 || ret != nil {
		t.Fatalf("bad sig ret=%x left=%d kind=%d", ret, left, kind)
	}
	if _, _, _, ok := runPrecompile(Address{19: 2}, input, gasEcrecover); ok {
		t.Fatal("expected ordinary address to miss the precompile table")
	}
}

// callEcrecoverCode 先在槽 3 写 1，再把调用数据交给 0x01。
// 槽 0 是这次调用的结果，槽 1 是返回数据长度，槽 2 是返回数据的第一个字。
// CALL 和 CALLCODE 多压一个金额 0。DELEGATECALL 和 STATICCALL 没有金额。
func callEcrecoverCode(gas uint16, op byte) []byte {
	// 调用从栈顶往下弹：gas、地址、金额、输入偏移、输入长度、返回偏移、返回长度。
	// 所以先压返回长度，最后压 gas。
	code := []byte{
		0x60, 0x01, 0x60, 0x03, 0x55, // PUSH1 1, PUSH1 3, SSTORE。槽 3 记下调用前的标记
		0x36, 0x60, 0x00, 0x60, 0x00, 0x37, // CALLDATASIZE, PUSH1 0, PUSH1 0, CALLDATACOPY。输入拷到内存 0
		0x60, 0x20, // PUSH1 32。返回长度
		0x60, 0x00, // PUSH1 0。返回数据写到内存 0，盖住原来的输入
		0x60, 0x80, // PUSH1 128。输入长度
		0x60, 0x00, // PUSH1 0。输入从内存 0 读
	}
	if op == opCALL || op == opCALLCODE {
		code = append(code, 0x60, 0x00) // PUSH1 0。这两种调用要带金额，这里是 0
	}
	code = append(code, 0x73) // PUSH20
	code = append(code, precompileEcrecover[:]...)
	code = append(code, 0x61, byte(gas>>8), byte(gas), op) // PUSH2 gas，再执行这次调用
	code = append(code,
		0x60, 0x00, 0x55, // PUSH1 0, SSTORE。槽 0 记下栈上的 1 或 0
		0x3d, 0x60, 0x01, 0x55, // RETURNDATASIZE, PUSH1 1, SSTORE。槽 1 是返回长度
		0x60, 0x00, 0x51, 0x60, 0x02, 0x55, // PUSH1 0, MLOAD, PUSH1 2, SSTORE。槽 2 是返回的第一个字
		0x00, // STOP
	)
	return code
}

// runEcrecoverCall 在 caller 上跑 code。外层 gas 给足，内层够不够由字节码里的 gas 参数决定。
func runEcrecoverCall(t *testing.T, st *State, caller Address, code, input []byte) execKind {
	t.Helper()
	steps := maxSteps
	_, _, _, kind := runCode(st, caller, Address{}, U256(0), input, code, 200000, 1, &steps)
	return kind
}

// 合约 CALL 0x01。成功时槽 0 是 1，槽 1 是 32，槽 2 是恢复出的地址，槽 3 的标记还在。
// 0x01 上写了 SSTORE 也不会执行。STATICCALL 同样能恢复地址。
// 签名被改过时调用仍成功，返回长度和地址都是 0。内层只给 2999 gas 时槽 0 是 0，调用方继续。
func TestCallEcrecover(t *testing.T) {
	hash, signature, addr := demoSignedHash(t)
	input := ecrecoverInput(hash[:], signature)
	var caller Address
	caller[19] = 9

	st := NewState()
	// 0x01 上即使写了代码，也走预编译，不执行这段 SSTORE。
	st.SetCode(precompileEcrecover, []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x00})
	kind := runEcrecoverCall(t, st, caller, callEcrecoverCode(3000, opCALL), input)
	if kind != execOK {
		t.Fatalf("kind=%d", kind)
	}
	if st.GetStorage(caller, Hash{}) != hashFromUint64(1) {
		t.Fatalf("call result = %x", st.GetStorage(caller, Hash{}))
	}
	if st.GetStorage(caller, hashFromUint64(1)) != hashFromUint64(32) {
		t.Fatalf("return size = %x", st.GetStorage(caller, hashFromUint64(1)))
	}
	if st.GetStorage(caller, hashFromUint64(2)) != hashFromAddress(addr) {
		t.Fatalf("address = %x", st.GetStorage(caller, hashFromUint64(2)))
	}
	if st.GetStorage(caller, hashFromUint64(3)) != hashFromUint64(1) {
		t.Fatal("marker was reverted")
	}
	if st.GetStorage(precompileEcrecover, Hash{}) != (Hash{}) {
		t.Fatal("precompile ran account code")
	}

	// STATICCALL 同样按地址查表。
	st = NewState()
	kind = runEcrecoverCall(t, st, caller, callEcrecoverCode(3000, opSTATICCALL), input)
	if kind != execOK || st.GetStorage(caller, hashFromUint64(2)) != hashFromAddress(addr) {
		t.Fatalf("static address = %x kind=%d", st.GetStorage(caller, hashFromUint64(2)), kind)
	}

	// 签名被改过：调用成功，返回数据是空的，调用前写入的槽还在。
	bad := bytes.Clone(input)
	bad[80] ^= 0x01
	st = NewState()
	kind = runEcrecoverCall(t, st, caller, callEcrecoverCode(3000, opCALL), bad)
	if kind != execOK {
		t.Fatalf("bad sig kind=%d", kind)
	}
	if st.GetStorage(caller, Hash{}) != hashFromUint64(1) {
		t.Fatalf("bad sig result = %x", st.GetStorage(caller, Hash{}))
	}
	if st.GetStorage(caller, hashFromUint64(1)) != (Hash{}) || st.GetStorage(caller, hashFromUint64(2)) != (Hash{}) {
		t.Fatal("expected empty return")
	}
	if st.GetStorage(caller, hashFromUint64(3)) != hashFromUint64(1) {
		t.Fatal("bad signature reverted the caller")
	}

	// 转出的 gas 不够 3000：这次调用失败，栈上是 0，调用方继续。
	st = NewState()
	kind = runEcrecoverCall(t, st, caller, callEcrecoverCode(2999, opCALL), input)
	if kind != execOK {
		t.Fatalf("low gas kind=%d", kind)
	}
	if st.GetStorage(caller, Hash{}) != (Hash{}) {
		t.Fatalf("low gas result = %x", st.GetStorage(caller, Hash{}))
	}
	if st.GetStorage(caller, hashFromUint64(3)) != hashFromUint64(1) {
		t.Fatal("low gas halted the caller")
	}
}

// 带金额的 CALL，输入是空的：恢复失败但调用成功，金额 4 留在 0x01。
// 请求 699 再加带金额调用的 2300，子调用一共 2999，仍不够 3000。调用失败，两边余额回到调用前。
func TestCallEcrecoverValueRevertsOnlyOnOutOfGas(t *testing.T) {
	var caller Address
	caller[19] = 9
	// 金额 4，gas 3000。签名是空的，恢复失败，但转账留下。
	// 栈的压入顺序和 callEcrecoverCode 相同，这里输入长度是 0。
	code := []byte{0x60, 0x20, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x04, 0x73}
	code = append(code, precompileEcrecover[:]...)
	code = append(code, 0x61, 0x0b, 0xb8, opCALL, 0x60, 0x00, 0x55, 0x00) // PUSH2 3000, CALL，槽 0 记下结果
	st := NewState()
	st.SetAccount(Account{Address: caller, Balance: U256(10)})
	if kind := runEcrecoverCall(t, st, caller, code, nil); kind != execOK {
		t.Fatalf("kind=%d", kind)
	}
	if st.GetStorage(caller, Hash{}) != hashFromUint64(1) {
		t.Fatalf("result = %x", st.GetStorage(caller, Hash{}))
	}
	callerBal, _ := st.GetBalanceNonce(caller)
	got, _ := st.GetBalanceNonce(precompileEcrecover)
	if callerBal != U256(6) || got != U256(4) {
		t.Fatalf("balances caller=%s precompile=%s", callerBal, got)
	}

	// 带金额的 CALL 会再给 2300。请求 699 时，子调用一共 2999，仍不够 3000。调用失败，两方余额回到调用前。
	low := []byte{0x60, 0x20, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x04, 0x73}
	low = append(low, precompileEcrecover[:]...)
	low = append(low, 0x61, 0x02, 0xbb, opCALL, 0x60, 0x00, 0x55, 0x00) // PUSH2 699, CALL
	st = NewState()
	st.SetAccount(Account{Address: caller, Balance: U256(10)})
	if kind := runEcrecoverCall(t, st, caller, low, nil); kind != execOK {
		t.Fatalf("low kind=%d", kind)
	}
	if st.GetStorage(caller, Hash{}) != (Hash{}) {
		t.Fatal("expected failed call")
	}
	callerBal, _ = st.GetBalanceNonce(caller)
	got, _ = st.GetBalanceNonce(precompileEcrecover)
	if callerBal != U256(10) || got != U256(0) {
		t.Fatalf("reverted balances caller=%s precompile=%s", callerBal, got)
	}
}

// 交易直接打到 0x01。签名不合法仍成功入块，金额留下，gas 在起步 gas 之外再扣 3000。
// 剩下的 gas 比 3000 少 1 时整笔停机：金额退回，nonce 加 1，费用按整个 gasLimit 收。
func TestApplyEcrecoverChargesWithoutReverting(t *testing.T) {
	hash, signature, _ := demoSignedHash(t)
	input := ecrecoverInput(hash[:], signature)
	bad := bytes.Clone(input)
	bad[80] ^= 0x01
	st, _ := funded(t, 1000000, 0)
	key := demoKey(t)
	probe := Tx{Data: bad}
	intrinsic, err := probe.intrinsicGas()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := SignCall(key, precompileEcrecover, 5, 0, intrinsic+gasEcrecover, 1, ChainID, bad)
	if err != nil {
		t.Fatal(err)
	}
	receipt := mustApply(t, st, tx)
	if receipt.Status != 1 || receipt.GasUsed != intrinsic+gasEcrecover {
		t.Fatalf("receipt = %+v", receipt)
	}
	from := demoAddr(t)
	bal, nonce := st.GetBalanceNonce(from)
	// 费用是 gasUsed * 1。金额 5 已经转到 0x01，签名失败并不退回。
	if nonce != 1 || bal != U256(1000000-5-receipt.GasUsed) {
		t.Fatalf("sender balance=%s nonce=%d", bal, nonce)
	}
	got, _ := st.GetBalanceNonce(precompileEcrecover)
	if got != U256(5) {
		t.Fatalf("precompile balance = %s", got)
	}

	// 剩下的 gas 比 3000 少 1。整笔停机，金额退回，nonce 仍然加 1，费用按 gasLimit 收。
	low, err := SignCall(key, precompileEcrecover, 5, 1, intrinsic+gasEcrecover-1, 1, ChainID, input)
	if err != nil {
		t.Fatal(err)
	}
	receipt = mustApply(t, st, low)
	if receipt.Status != 0 || receipt.GasUsed != low.GasLimit {
		t.Fatalf("low receipt = %+v", receipt)
	}
	bal, nonce = st.GetBalanceNonce(from)
	if nonce != 2 || bal != U256(1000000-5-intrinsic-gasEcrecover-low.GasLimit) {
		t.Fatalf("after halt balance=%s nonce=%d", bal, nonce)
	}
	got, _ = st.GetBalanceNonce(precompileEcrecover)
	if got != U256(5) {
		t.Fatalf("halt kept value, balance = %s", got)
	}
}
