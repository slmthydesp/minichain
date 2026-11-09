package chain

import (
	"math/big"
	"testing"

	"github.com/slmthydesp/minichain/sig"
)

// 这个文件测虚拟机：字运算、跳转、内存和存储、创建与调用、回退和日志。
// 多数用例用一小段字节码，看槽位或回执里留下什么。

// hashMod 把任意大整数收到 32 字节里，按 2^256 取模。
// 1. 基础正确性：零值、小正数、跨字节数，验证右对齐和大端序逻辑是否正确
// 2. 上边界场景：2^256-1（最大值不溢出）、2^256（刚好溢出归零）、溢出正数，验证取模的截断效果
// 3. 负数场景：-1（最典型下溢）、普通负数、负半区最小值，验证负数转同余正数的核心逻辑
// 4. 位对齐验证：2^255 最高位场景，验证字节位置是否严格符合 uint256 大端规范
func TestHashMod(t *testing.T) {
	// 预计算模数 2^256，用于构造边界测试用例
	mod := new(big.Int).Lsh(big.NewInt(1), 256)

	// 预构造 32 字节全 0xff 的结果，对应 2^256 - 1
	var fullFF Hash
	for i := range fullFF {
		fullFF[i] = 0xff
	}

	tests := []struct {
		name  string   // 用例名称
		input *big.Int // 输入 hashMod 的大整数
		want  Hash     // 预期 32 字节结果
	}{
		{
			name:  "零值输入",
			input: big.NewInt(0),
			want:  Hash{}, // 全 0 字节
		},
		{
			name:  "小正整数",
			input: big.NewInt(1),
			want: func() Hash {
				var h Hash
				h[31] = 0x01 // 大端序：最低位在最后一个字节
				return h
			}(),
		},
		{
			name:  "跨字节正整数 256 = 0x100",
			input: big.NewInt(256),
			want: func() Hash {
				var h Hash
				h[30] = 0x01 // 倒数第二个字节存高位
				h[31] = 0x00
				return h
			}(),
		},
		{
			name:  "256位最大值 2^256 - 1（上边界）",
			input: new(big.Int).Sub(mod, big.NewInt(1)),
			want:  fullFF,
		},
		{
			name:  "刚好溢出 2^256，模后归零",
			input: mod,
			want:  Hash{},
		},
		{
			name:  "溢出正数 2^256 + 5，模后等价于 5",
			input: new(big.Int).Add(mod, big.NewInt(5)),
			want: func() Hash {
				var h Hash
				h[31] = 0x05
				return h
			}(),
		},
		{
			name:  "负数 -1，对应 EVM 减法下溢",
			input: big.NewInt(-1),
			want:  fullFF, // -1 mod 2^256 = 2^256 - 1
		},
		{
			name:  "负数 -5，普通负整数转换",
			input: big.NewInt(-5),
			want: func() Hash {
				h := fullFF
				h[31] = 0xfb // 2^256 -5，最低字节为 0xfb
				return h
			}(),
		},
		{
			name:  "负数 -2^255，负半区最小值",
			input: new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 255)),
			want: func() Hash {
				var h Hash
				h[0] = 0x80 // 模 2^256 后等价于 2^255，最高位为 1
				return h
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hashMod(tt.input)
			if got != tt.want {
				t.Errorf("hashMod() = %x\nwant      = %x", got, tt.want)
			}
		})
	}
}

// initOf 拼一段创建代码：把 runtime 拷进内存再 RETURN，部署后合约代码就是 runtime。
// 前缀固定 12 字节：PUSH1 n / PUSH1 12 / PUSH1 0 / CODECOPY / PUSH1 n / PUSH1 0 / RETURN。
func initOf(runtime []byte) []byte {
	n := byte(len(runtime))
	// COPYCODE：destOffset, offset, length
	// RETURN：offset, length
	// PUSH1 n PUSH1 12 PUSH1 0 CODECOPY PUSH1 n PUSH1 0 RETURN
	pref := []byte{0x60, n, 0x60, 0x0c, 0x60, 0x00, 0x39, 0x60, n, 0x60, 0x00, 0xf3}
	return append(pref, runtime...)
}

// demoAddr 是演示私钥对应的地址。
func demoAddr(t *testing.T) Address {
	t.Helper()
	raw, err := sig.KeyAddress(demoKey(t))
	if err != nil {
		t.Fatal(err)
	}
	var addr Address
	copy(addr[:], raw[:])
	return addr
}

// mustApply 执行一笔交易。Apply 自己报错时测试立刻失败。
func mustApply(t *testing.T, st *State, tx Tx) Receipt {
	t.Helper()
	receipt, err := Apply(st, tx)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

// codeJumpIf 把 cond 接到一段 JUMPI 上，用槽 0 记下跳没跳中。
// cond 负责留下栈顶：全 0 视为假，其余为真。真则合约存储槽 0 存 1，假则存 0。
func codeJumpIf(cond []byte) []byte {
	// JUMPDEST 下标是 len(cond)+9：PUSH1 dest 和 JUMPI 占 3 字节，未跳中的「存 0 / STOP」占 6 字节。
	dest := len(cond) + 9
	code := append([]byte{}, cond...)
	// 再压跳转目标。JUMPI 先弹目标，再弹 cond 留下的条件。
	code = append(code, 0x60, byte(dest), 0x57)
	// 条件为 0：顺序执行到这里，槽 0 写成 0 后停机。
	code = append(code, 0x60, 0x00, 0x60, 0x00, 0x55, 0x00)
	// 条件非 0：跳进这条 JUMPDEST，槽 0 写成 1 后停机。
	// code PUSH1 dest JUMPI PUSH1 0 PUSH1 0 SSTORE STOP JUMPDEST PUSH1 1 PUSH1 0 SSTORE STOP
	code = append(code, 0x5b, 0x60, 0x01, 0x60, 0x00, 0x55, 0x00)
	return code
}

// runSlot0 在空状态上跑一段代码，返回合约槽 0 和这一帧的结果种类。
func runSlot0(t *testing.T, code []byte) (Hash, execKind) {
	t.Helper()
	st := NewState()
	var addr Address
	addr[19] = 1
	steps := maxSteps
	_, _, _, kind := runCode(st, addr, Address{}, U256(0), nil, code, 100000, 1, &steps)
	return st.GetStorage(addr, Hash{}), kind
}

// EQ、LT、ISZERO 的结果交给 JUMPI：真则槽 0 存 1，假则存 0。
func TestCompareDecidesJump(t *testing.T) {
	// 32 字节最高位为 1，数值是 2^248，用来测宽字比较，不是只看最低字节。
	big := make([]byte, 32)
	big[0] = 0x01
	// PUSH32 0x0100...0000 PUSH1 0x01 LT
	ltSmall := append(append([]byte{0x7f}, big...), 0x60, 0x01, 0x10)
	// PUSH1 0x01 PUSH32 0x0100...0000 LT
	ltBig := append(append([]byte{0x60, 0x01, 0x7f}, big...), 0x10)
	// PUSH32 0x0100...0000 ISZERO
	isZeroBig := append(append([]byte{0x7f}, big...), 0x15)
	tests := []struct {
		name string
		cond []byte
		want uint64
	}{
		// PUSH1 0x01 PUSH1 0x01 EQ
		{name: "eq", cond: []byte{0x60, 0x01, 0x60, 0x01, 0x14}, want: 1},
		// PUSH1 0x01 PUSH1 0x02 EQ
		{name: "ne", cond: []byte{0x60, 0x01, 0x60, 0x02, 0x14}, want: 0},
		// PUSH1 0x02 PUSH1 0x01 LT
		{name: "lt", cond: []byte{0x60, 0x02, 0x60, 0x01, 0x10}, want: 1},
		// PUSH1 0x01 PUSH1 0x02 LT
		{name: "not lt", cond: []byte{0x60, 0x01, 0x60, 0x02, 0x10}, want: 0},
		{name: "lt wide", cond: ltSmall, want: 1},
		{name: "not lt wide", cond: ltBig, want: 0},
		// PUSH1 0x00 ISZERO
		{name: "iszero", cond: []byte{0x60, 0x00, 0x15}, want: 1},
		// PUSH1 0x01 ISZERO
		{name: "not iszero", cond: []byte{0x60, 0x01, 0x15}, want: 0},
		{name: "wide is not zero", cond: isZeroBig, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := codeJumpIf(tt.cond)
			// JUMPI 的目标按布局应是 cond 之后第 9 字节的 JUMPDEST。拼错了后面槽 0 的值就没有意义。
			if code[len(tt.cond)+9] != 0x5b {
				t.Fatalf("jump dest byte = %x", code[len(tt.cond)+9])
			}
			// 跑完整段：比较为真应跳中并存 1，为假应落在未跳中分支并存 0。
			got, kind := runSlot0(t, code)
			if kind != execOK {
				t.Fatalf("kind = %d", kind)
			}
			if got != hashFromUint64(tt.want) {
				t.Fatalf("storage = %x", got)
			}
		})
	}
}

// PUSH 立即数里的 0x5b 不能当跳转目标；真正的 JUMPDEST 字节可以。
func TestPushImmediateJumpDestIsNotTarget(t *testing.T) {
	// 跳进 PUSH1 的立即数。那个字节是 0x5b，看起来像 JUMPDEST，但不能当目标。
	// 目标 5 落在 PUSH1 的立即数 0x5b 上，不是下标 6 的 JUMPDEST。
	// 0: PUSH1 0x05  2: JUMP  3: STOP  4: PUSH1 0x5b  6: JUMPDEST  7: PUSH1 0x01  9: PUSH1 0x00  11: SSTORE  12: STOP
	bad := []byte{0x60, 0x05, 0x56, 0x00, 0x60, 0x5b, 0x5b, 0x60, 0x01, 0x60, 0x00, 0x55, 0x00}
	got, kind := runSlot0(t, bad)
	if kind != execHalt {
		t.Fatalf("kind = %d", kind)
	}
	if got != (Hash{}) {
		t.Fatalf("storage = %x", got)
	}
	// 同一段布局里，真正的 JUMPDEST 仍然可以跳进去并存 1。
	// 0: PUSH1 0x06  2: JUMP  3: STOP  4: PUSH1 0x5b  6: JUMPDEST  7: PUSH1 0x01  9: PUSH1 0x00  11: SSTORE  12: STOP
	ok := []byte{0x60, 0x06, 0x56, 0x00, 0x60, 0x5b, 0x5b, 0x60, 0x01, 0x60, 0x00, 0x55, 0x00}
	got, kind = runSlot0(t, ok)
	if kind != execOK {
		t.Fatalf("kind = %d", kind)
	}
	if got != hashFromUint64(1) {
		t.Fatalf("storage = %x", got)
	}
}

// addressWord 把 20 字节地址放到 32 字节字的右边，和栈上 ADDRESS / CALLER 的排法一样。
func addressWord(a Address) Hash {
	var h Hash
	copy(h[12:], a[:])
	return h
}

// contextCode 把 CALLER、ADDRESS、CALLVALUE、NUMBER、TIMESTAMP 依次存进槽 0 到 4。
func contextCode() []byte {
	// https://www.evm.codes/playground 切换输入 Bytecode 数据
	// 输入：0x336000553060015534600255436003554260045500
	return []byte{
		0x33, 0x60, 0x00, 0x55, // CALLER PUSH1 0 SSTORE
		0x30, 0x60, 0x01, 0x55, // ADDRESS PUSH1 1 SSTORE
		0x34, 0x60, 0x02, 0x55, // CALLVALUE PUSH1 2 SSTORE
		0x43, 0x60, 0x03, 0x55, // NUMBER PUSH1 3 SSTORE
		0x42, 0x60, 0x04, 0x55, // TIMESTAMP PUSH1 4 SSTORE
		0x00, // STOP
	}
}

// 成功调用能读到 CALLER、本地址、附带金额，以及当前块的高度和时间。
func TestCallContextOnStack(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	key := demoKey(t)
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(contextCode()))
	if err != nil {
		t.Fatal(err)
	}
	if receipt := mustApply(t, st, create); receipt.Status != 1 {
		t.Fatalf("create = %+v", receipt)
	}
	// 0x5fbdb2315678afecb367f032d93f642f64180aa3
	addr := CreateAddress(from, 0)
	// 再部署一个一调用就 REVERT 的合约并调用它。回退完成后，下一笔成功调用读到的仍是自己的调用方、金额和当前块的高度、时间。
	// PUSH1 0 PUSH1 0 REVERT。这是运行时代码，创建时由 initOf 包起来，所以部署会成功。
	revertCode := []byte{0x60, 0x00, 0x60, 0x00, 0xfd}
	// 金额 0，nonce 1（nonce 0 已用于上面的合约），gas 90000，单价 1。
	revertCreate, err := SignCreate(key, 0, 1, 90000, 1, ChainID, initOf(revertCode))
	if err != nil {
		t.Fatal(err)
	}
	// 创建成功，回执状态为 1。地址由发送方和 nonce 1 算出。
	if receipt := mustApply(t, st, revertCreate); receipt.Status != 1 {
		t.Fatalf("revert create = %+v", receipt)
	}
	// 0xe7f1725e7734ce288f8367e1bb143e90bb3f0512
	reverter := CreateAddress(from, 1)
	// 当前块高度 7、时间 99。后面两笔交易都在这一拍上执行。
	st.SetBlock(7, 99)
	// nonce 2，金额 0，gas 40000。调用 reverter，运行时代码立刻 REVERT。
	revertTx, err := Sign(key, reverter, 0, 2, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	// 回执状态 0：交易入块，执行中的状态修改已回滚。
	if receipt := mustApply(t, st, revertTx); receipt.Status != 0 {
		t.Fatalf("revert = %+v", receipt)
	}
	// nonce 3，附带 9，gas 80000。调用第一份合约，它把五项上下文写入槽 0 到 4。
	call, err := Sign(key, addr, 9, 3, 80000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt := mustApply(t, st, call); receipt.Status != 1 {
		t.Fatalf("call = %+v", receipt)
	}
	// 槽 0 是 CALLER，即这笔交易的发送方。
	if st.GetStorage(addr, hashFromUint64(0)) != addressWord(from) {
		t.Fatalf("caller = %x", st.GetStorage(addr, hashFromUint64(0)))
	}
	// 槽 1 是 ADDRESS，即合约自己的地址。
	if st.GetStorage(addr, hashFromUint64(1)) != addressWord(addr) {
		t.Fatalf("address = %x", st.GetStorage(addr, hashFromUint64(1)))
	}
	// 槽 2 是 CALLVALUE，即这笔附带的 9。
	if st.GetStorage(addr, hashFromUint64(2)) != hashFromUint64(9) {
		t.Fatalf("value = %x", st.GetStorage(addr, hashFromUint64(2)))
	}
	// 槽 3 是 NUMBER，即 SetBlock 设的高度 7。
	if st.GetStorage(addr, hashFromUint64(3)) != hashFromUint64(7) {
		t.Fatalf("number = %x", st.GetStorage(addr, hashFromUint64(3)))
	}
	// 槽 4 是 TIMESTAMP，即 SetBlock 设的时间 99。
	if st.GetStorage(addr, hashFromUint64(4)) != hashFromUint64(99) {
		t.Fatalf("timestamp = %x", st.GetStorage(addr, hashFromUint64(4)))
	}
}

// 内层 CALL 看到的 CALLER 是外层合约，CALLVALUE 是这次 CALL 附带的金额，不是交易金额。
func TestInnerCallContext(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	key := demoKey(t)
	// ======= 1. 部署 callee 合约代码 =======
	// CALLER PUSH1 0 SSTORE CALLVALUE PUSH1 1 SSTORE STOP。槽 0 是调用方，槽 1 是本次调用金额。
	calleeCode := []byte{0x33, 0x60, 0x00, 0x55, 0x34, 0x60, 0x01, 0x55, 0x00}
	// 金额 0，nonce 0，gas 90000，单价 1。部署被调用方。
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(calleeCode))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, create)
	// 地址由发送方和 nonce 0 算出。
	callee := CreateAddress(from, 0)

	// ======= 2. 部署 caller 合约代码 =======
	// 先压返回长度、返回偏移、输入长度、输入偏移，都是 0，再压金额 5，接着 PUSH20 被调用方地址。
	callerCode := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x05, 0x73}
	callerCode = append(callerCode, callee[:]...)
	// PUSH2 0x4e20 是 gas 20000，然后 CALL、STOP。CALL 弹出的依次是 gas、地址、金额和输入输出区间。
	callerCode = append(callerCode, 0x61, 0x4e, 0x20, 0xf1, 0x00)
	// nonce 1，部署这段调用方代码。
	// callerCode: PUSH1 0 PUSH1 0 PUSH1 0 PUSH1 0 PUSH1 5 PUSH20 callee PUSH2 0x4e20 CALL STOP
	create, err = SignCreate(key, 0, 1, 90000, 1, ChainID, initOf(callerCode))
	if err != nil {
		t.Fatal(err)
	}
	// 创建成功，回执状态为 1。
	if receipt := mustApply(t, st, create); receipt.Status != 1 {
		t.Fatalf("caller create = %+v", receipt)
	}
	caller := CreateAddress(from, 1)

	// ======= 3. 调用 caller 合约 =======
	// nonce 2，交易附带 10，gas 80000。打到外层合约，由它再 CALL 内层并只转 5。
	call, err := Sign(key, caller, 10, 2, 80000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt := mustApply(t, st, call); receipt.Status != 1 {
		t.Fatalf("call = %+v", receipt)
	}

	// ======= 4. 检查 callee 合约的槽 0 和槽 1 =======
	// 槽 0 是内层 CALLER，即外层合约 caller。
	if st.GetStorage(callee, hashFromUint64(0)) != addressWord(caller) {
		t.Fatalf("inner caller = %x", st.GetStorage(callee, hashFromUint64(0)))
	}
	// 槽 1 是内层 CALLVALUE，即 CALL 附带的 5。
	if st.GetStorage(callee, hashFromUint64(1)) != hashFromUint64(5) {
		t.Fatalf("inner value = %x", st.GetStorage(callee, hashFromUint64(1)))
	}
}

// 挖出来的块里 NUMBER、TIMESTAMP、CALLVALUE 和区块头一致。另一条链 Append 同一批块，读到的值相同。
func TestMinedBlockContextMatchesAppend(t *testing.T) {
	key := demoKey(t)
	// nonce 0。contextCode 把 CALLVALUE、NUMBER、TIMESTAMP 写入槽 2、3、4。
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(contextCode()))
	if err != nil {
		t.Fatal(err)
	}
	a := New()
	if _, err := a.AddTx(create); err != nil {
		t.Fatal(err)
	}
	// 创世高度 0、时间 0。这笔创建挖进高度 1、时间 1 的块。
	if _, err := a.Mine(Address{}, 1); err != nil {
		t.Fatal(err)
	}
	addr := CreateAddress(demoAddr(t), 0)
	// nonce 1，附带 4，gas 80000。下一块应是高度 2、时间 40。
	call, err := Sign(key, addr, 4, 1, 80000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddTx(call); err != nil {
		t.Fatal(err)
	}
	block, err := a.Mine(Address{}, 40)
	if err != nil {
		t.Fatal(err)
	}
	// 一块里只有这笔调用，回执状态为 1。
	if block.Height != 2 || block.Time != 40 || len(block.Receipts) != 1 || block.Receipts[0].Status != 1 {
		t.Fatalf("block = %+v", block)
	}
	a.mu.Lock()
	// 槽 3、4、2 分别是 NUMBER、TIMESTAMP、CALLVALUE。
	number := a.state.GetStorage(addr, hashFromUint64(3))
	ts := a.state.GetStorage(addr, hashFromUint64(4))
	value := a.state.GetStorage(addr, hashFromUint64(2))
	a.mu.Unlock()
	if number != hashFromUint64(block.Height) || ts != hashFromUint64(block.Time) || value != hashFromUint64(4) {
		t.Fatalf("number=%x time=%x value=%x", number, ts, value)
	}
	// 从创世块重放 a 的全部块，槽 3 和槽 4 应与链 a 相同。
	b := New()
	for _, blk := range a.BlocksAfter(0) {
		if err := b.Append(blk); err != nil {
			t.Fatal(err)
		}
	}
	b.mu.Lock()
	gotN := b.state.GetStorage(addr, hashFromUint64(3))
	gotT := b.state.GetStorage(addr, hashFromUint64(4))
	b.mu.Unlock()
	if gotN != number || gotT != ts {
		t.Fatalf("appended number=%x time=%x", gotN, gotT)
	}
}

// SUB 先弹栈顶再弹下一字：3 - 1 存进槽 0。
func TestWordArithmeticStoresDifference(t *testing.T) {
	st := NewState()
	var addr Address
	addr[19] = 1
	// PUSH1 1 PUSH1 3 SUB PUSH1 0 SSTORE STOP。栈顶 3 减下一字 1，差写入槽 0。
	code := []byte{0x60, 0x01, 0x60, 0x03, 0x03, 0x60, 0x00, 0x55, 0x00}
	steps := maxSteps
	// 直接跑这段代码，不经过交易。gas 100000，调用深度 1。
	_, _, _, kind := runCode(st, addr, Address{}, U256(0), nil, code, 100000, 1, &steps)
	if kind != execOK {
		t.Fatalf("kind = %d", kind)
	}
	// 槽 0 是 3 - 1 = 2。
	if st.GetStorage(addr, Hash{}) != hashFromUint64(2) {
		t.Fatalf("storage = %x", st.GetStorage(addr, Hash{}))
	}
}

// ADD 得到 1+3=4。全 1 的字再加 1 按 2^256 回绕成 0，ISZERO 为真。
func TestAddStoresSumAndWraps(t *testing.T) {
	// PUSH1 1 PUSH1 3 ADD PUSH1 0 SSTORE STOP。和 4 写入槽 0。
	got, kind := runSlot0(t, []byte{0x60, 0x01, 0x60, 0x03, 0x01, 0x60, 0x00, 0x55, 0x00})
	if kind != execOK || got != hashFromUint64(4) {
		t.Fatalf("kind=%d storage=%x", kind, got)
	}
	// 32 字节全 0xff，即 2^256-1。
	ones := make([]byte, 32)
	for i := range ones {
		ones[i] = 0xff
	}
	// PUSH32 ones PUSH1 1 ADD ISZERO。加 1 回绕成 0，ISZERO 留下 1。
	cond := append([]byte{0x7f}, ones...)
	cond = append(cond, 0x60, 0x01, 0x01, 0x15)
	// codeJumpIf 看到条件为真，把 1 写入槽 0。
	got, kind = runSlot0(t, codeJumpIf(cond))
	if kind != execOK || got != hashFromUint64(1) {
		t.Fatalf("wrap kind=%d storage=%x", kind, got)
	}
}

// 没写过的内存是 0；MSTORE 再 MLOAD，读回刚才写入的值。
func TestMemoryRoundTrip(t *testing.T) {
	// PUSH1 0 MLOAD ISZERO。偏移 0 还没写过，读出 0，ISZERO 为真，槽 0 写入 1。
	got, kind := runSlot0(t, codeJumpIf([]byte{0x60, 0x00, 0x51, 0x15}))
	if kind != execOK || got != hashFromUint64(1) {
		t.Fatalf("empty kind=%d storage=%x", kind, got)
	}
	// PUSH1 0x2a PUSH1 0 MSTORE PUSH1 0 MLOAD PUSH1 0 SSTORE STOP。写进内存再读回槽 0。
	code := []byte{0x60, 0x2a, 0x60, 0x00, 0x52, 0x60, 0x00, 0x51, 0x60, 0x00, 0x55, 0x00}
	got, kind = runSlot0(t, code)
	// 槽 0 是刚写入的 0x2a。
	if kind != execOK || got != hashFromUint64(0x2a) {
		t.Fatalf("round trip kind=%d storage=%x", kind, got)
	}
}

// SLOAD 读出事先写入的槽；没写过的槽是 0。
func TestSloadReadsStoredSlot(t *testing.T) {
	st := NewState()
	var addr Address
	addr[19] = 1
	// 槽 0 事先写成 7，代码本身不再写它。
	st.SetStorage(addr, Hash{}, hashFromUint64(7))
	// PUSH1 0 SLOAD PUSH1 1 SSTORE：把槽 0 抄到槽 1。
	// PUSH1 2 SLOAD ISZERO PUSH1 3 SSTORE：槽 2 没写过，读出 0，ISZERO 的 1 写入槽 3。
	code := []byte{0x60, 0x00, 0x54, 0x60, 0x01, 0x55, 0x60, 0x02, 0x54, 0x15, 0x60, 0x03, 0x55, 0x00}
	steps := maxSteps
	_, _, _, kind := runCode(st, addr, Address{}, U256(0), nil, code, 100000, 1, &steps)
	if kind != execOK {
		t.Fatalf("kind = %d", kind)
	}
	// 槽 1 是从槽 0 读出的 7。
	if st.GetStorage(addr, hashFromUint64(1)) != hashFromUint64(7) {
		t.Fatalf("loaded = %x", st.GetStorage(addr, hashFromUint64(1)))
	}
	// 槽 3 是空槽的 ISZERO，为 1。
	if st.GetStorage(addr, hashFromUint64(3)) != hashFromUint64(1) {
		t.Fatalf("empty = %x", st.GetStorage(addr, hashFromUint64(3)))
	}
}

// 调用数据按字节计入固有 gas：零字节 4、非零字节 16。21000+4+16=21020，少 1 就进不去。
func TestIntrinsicGasCountsCalldata(t *testing.T) {
	key := demoKey(t)
	var to Address
	to[19] = 2
	// data 是 0x00、0x01。金额 1，nonce 0，gasLimit 21019，比固有 gas 少 1。
	low, err := SignCall(key, to, 1, 0, 21019, 1, ChainID, []byte{0x00, 0x01})
	if err != nil {
		t.Fatal(err)
	}
	// 准入在执行前拒绝，没有回执。
	if _, err := Apply(NewState(), low); err == nil {
		t.Fatal("expected intrinsic gas")
	}
	// gasLimit 正好 21020。对方没有代码，这笔只做转账。
	ok, err := SignCall(key, to, 1, 0, 21020, 1, ChainID, []byte{0x00, 0x01})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := funded(t, 100000, 0)
	receipt := mustApply(t, st, ok)
	// 状态 1，烧掉的 gas 就是固有 gas 21020。
	if receipt.Status != 1 || receipt.GasUsed != 21020 {
		t.Fatalf("receipt = %+v", receipt)
	}
}

// 创建交易部署 runtime；随后调用把 1 写入槽 0。状态根应计入代码和存储。
func TestDeployAndCallStoresValue(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	key := demoKey(t)
	// PUSH1 1 PUSH1 0 SSTORE STOP。调用时把 1 写入槽 0。
	runtime := []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x00}
	// nonce 0，金额 0，gas 90000。initOf 包住 runtime 再部署。
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	receipt := mustApply(t, st, create)
	// 创建成功。链上代码是 runtime，不含前面的创建代码。
	if receipt.Status != 1 {
		t.Fatalf("create status = %d gas = %d", receipt.Status, receipt.GasUsed)
	}
	addr := CreateAddress(from, 0)
	if string(st.GetCode(addr)) != string(runtime) {
		t.Fatalf("code = %x", st.GetCode(addr))
	}
	// nonce 1，金额 0，gas 40000。调用刚部署的合约。
	call, err := Sign(key, addr, 0, 1, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt = mustApply(t, st, call)
	if receipt.Status != 1 {
		t.Fatalf("call status = %d", receipt.Status)
	}
	// 槽 0 是 1。
	if st.GetStorage(addr, Hash{}) != hashFromUint64(1) {
		t.Fatalf("storage = %x", st.GetStorage(addr, Hash{}))
	}
	// 代码和存储都进了状态根，根不再是空树。
	if root := StateRoot(st); root == emptyTrieRoot() {
		t.Fatal("state root ignored code and storage")
	}
}

// CALLDATALOAD 从偏移 0 读 32 字节。调用数据只有 0x2a，其余补 0，写入槽 0 后最高字节是 0x2a。
func TestCalldataIsStored(t *testing.T) {
	st, _ := funded(t, 1000000, 0)
	key := demoKey(t)
	// PUSH1 0 CALLDATALOAD PUSH1 0 SSTORE STOP。从偏移 0 取 32 字节写入槽 0。
	runtime := []byte{0x60, 0x00, 0x35, 0x60, 0x00, 0x55, 0x00}
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, create)
	addr := CreateAddress(demoAddr(t), 0)
	// nonce 1，data 只有一个字节 0x2a。
	call, err := SignCall(key, addr, 0, 1, 40000, 1, ChainID, []byte{0x2a})
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, call)
	got := st.GetStorage(addr, Hash{})
	// 调用数据不足 32 字节的位置补 0，所以字的最高字节是 0x2a。
	if got[0] != 0x2a {
		t.Fatalf("storage = %x", got)
	}
}

// REVERT 丢掉转账，合约余额仍是 0。nonce 照加，gas 费仍扣。
func TestRevertDropsValueAndKeepsFee(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	key := demoKey(t)
	// PUSH1 0 PUSH1 0 REVERT。一调用就回退。
	runtime := []byte{0x60, 0x00, 0x60, 0x00, 0xfd}
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, create)
	addr := CreateAddress(from, 0)
	// 记下发送方 nonce 和余额，回退后拿来比较。
	before, _ := st.GetAccount(from)
	// nonce 1，附带 10，gas 40000。
	call, err := Sign(key, addr, 10, 1, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt := mustApply(t, st, call)
	// 回执状态 0，没有日志。
	if receipt.Status != 0 || len(receipt.Logs) != 0 {
		t.Fatalf("receipt = %+v", receipt)
	}
	got, _ := st.GetAccount(from)
	// nonce 仍加 1。
	if got.Nonce != before.Nonce+1 {
		t.Fatalf("nonce = %d", got.Nonce)
	}
	// 余额只减去这笔烧掉的 gas，附带的 10 退回发送方。
	want, under := before.Balance.Sub(U256(receipt.GasUsed))
	if under || got.Balance != want {
		t.Fatalf("balance = %s want %s", got.Balance, want)
	}
	recip, _ := st.GetAccount(addr)
	// 合约没留下这笔 10，余额仍是 0。
	if !recip.Balance.IsZero() {
		t.Fatalf("contract balance = %d", recip.Balance)
	}
}

// 成功时 LOG1 留在回执里，并进入 receipts root。同一段日志后面接 REVERT，回执里没有日志。
func TestLogKeptOnSuccessAndDroppedOnRevert(t *testing.T) {
	st, _ := funded(t, 1000000, 0)
	key := demoKey(t)
	// PUSH1 0xab PUSH1 0 PUSH1 0 LOG1 STOP。topic 是 0xab，内存偏移和长度都是 0。
	runtime := []byte{0x60, 0xab, 0x60, 0x00, 0x60, 0x00, 0xa1, 0x00}
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, create)
	addr := CreateAddress(demoAddr(t), 0)
	// nonce 1，金额 0，gas 40000。
	call, err := Sign(key, addr, 0, 1, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt := mustApply(t, st, call)
	// 状态 1。一条日志，地址是合约自己。
	if receipt.Status != 1 || len(receipt.Logs) != 1 || receipt.Logs[0].Address != addr {
		t.Fatalf("receipt = %+v", receipt)
	}
	// topic 是压进去的 0xab。
	if receipt.Logs[0].Topics[0] != hashFromUint64(0xab) {
		t.Fatalf("topic = %x", receipt.Logs[0].Topics[0])
	}
	plain := receipt
	plain.Logs = nil
	// 同一张回执去掉日志后，回执根不同。日志参与了根的计算。
	if ReceiptsRoot([]Receipt{receipt}) == ReceiptsRoot([]Receipt{plain}) {
		t.Fatal("receipts root ignored logs")
	}

	// 去掉最后的 STOP，接上 PUSH1 0 PUSH1 0 REVERT。LOG1 先执行，随后这一帧回退。
	revertCode := append(append([]byte{}, runtime[:len(runtime)-1]...), 0x60, 0x00, 0x60, 0x00, 0xfd)
	st2, _ := funded(t, 1000000, 0)
	create, err = SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(revertCode))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st2, create)
	call, err = Sign(key, CreateAddress(demoAddr(t), 0), 0, 1, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	receipt = mustApply(t, st2, call)
	// 回执状态 0，日志为空。
	if receipt.Status != 0 || len(receipt.Logs) != 0 {
		t.Fatalf("reverted receipt = %+v", receipt)
	}
}

// 外层 CALL 跑被调用方代码。被调用方把 7 写入自己的槽 0。
func TestCallRunsCallee(t *testing.T) {
	st, from := funded(t, 1000000, 0)
	key := demoKey(t)
	// PUSH1 7 PUSH1 0 SSTORE STOP。
	calleeCode := []byte{0x60, 0x07, 0x60, 0x00, 0x55, 0x00}
	// nonce 0，部署被调用方。
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(calleeCode))
	if err != nil {
		t.Fatal(err)
	}
	mustApply(t, st, create)
	callee := CreateAddress(from, 0)
	// 返回区间、输入区间和金额都是 0，接着 PUSH20 被调用方地址。
	callerCode := []byte{0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x73}
	callerCode = append(callerCode, callee[:]...)
	// PUSH2 0x4e20 是 gas 20000，然后 CALL、STOP。
	callerCode = append(callerCode, 0x61, 0x4e, 0x20, 0xf1, 0x00)
	// nonce 1，部署调用方。
	create, err = SignCreate(key, 0, 1, 90000, 1, ChainID, initOf(callerCode))
	if err != nil {
		t.Fatal(err)
	}
	// 创建成功，回执状态为 1。
	if receipt := mustApply(t, st, create); receipt.Status != 1 {
		t.Fatalf("caller create = %+v", receipt)
	}
	caller := CreateAddress(from, 1)
	// nonce 2，金额 0，gas 80000。打到外层，由它 CALL 内层。
	call, err := Sign(key, caller, 0, 2, 80000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if receipt := mustApply(t, st, call); receipt.Status != 1 {
		t.Fatalf("call = %+v", receipt)
	}
	// 被调用方自己的槽 0 是 7。
	if st.GetStorage(callee, Hash{}) != hashFromUint64(7) {
		t.Fatalf("callee storage = %x", st.GetStorage(callee, Hash{}))
	}
}

// 回退的交易仍进块。回执失败，发送方 nonce 照样加一。
func TestMineIncludesRevertedTransaction(t *testing.T) {
	c := New()
	key := demoKey(t)
	// PUSH1 0 PUSH1 0 REVERT。
	runtime := []byte{0x60, 0x00, 0x60, 0x00, 0xfd}
	create, err := SignCreate(key, 0, 0, 90000, 1, ChainID, initOf(runtime))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(create); err != nil {
		t.Fatal(err)
	}
	// 创建交易挖进第一块。时间比创世的 0 晚 1。
	if _, err := c.Mine(Address{}, c.Head().Time+1); err != nil {
		t.Fatal(err)
	}
	addr := CreateAddress(demoAddr(t), 0)
	// nonce 1，附带 10，gas 40000。调用会 REVERT。
	call, err := Sign(key, addr, 10, 1, 40000, 1, ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddTx(call); err != nil {
		t.Fatal(err)
	}
	block, err := c.Mine(Address{}, c.Head().Time+1)
	if err != nil {
		t.Fatal(err)
	}
	// 第二块仍收进这笔交易。回执状态 0。
	if len(block.Txs) != 1 || block.Receipts[0].Status != 0 {
		t.Fatalf("block txs=%d receipt=%+v", len(block.Txs), block.Receipts)
	}
	acct, _ := c.Account(demoAddr(t))
	// 创建和这次回退都入块，发送方 nonce 加到 2。
	if acct.Nonce != 2 {
		t.Fatalf("nonce = %d", acct.Nonce)
	}
}
