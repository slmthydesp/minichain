package chain

import (
	"encoding/binary"
	"errors"
	"math/big"
)

// 操作码编号、弹出顺序和结果对齐 Osaka 的传统执行，不含 EOF。
// 对照表：https://www.evm.codes/?fork=osaka
// 和以太坊仍有这些差别：gas 是固定价，不随冷热和硬分叉变化；
// 这条链没有 blob、base fee 和 prevrandao，对应操作码读出来是 0。
const (
	// 0x00 算术。停机、加减乘除、取模、幂和符号扩展。
	opSTOP       = 0x00
	opADD        = 0x01
	opMUL        = 0x02
	opSUB        = 0x03
	opDIV        = 0x04
	opSDIV       = 0x05
	opMOD        = 0x06
	opSMOD       = 0x07
	opADDMOD     = 0x08
	opMULMOD     = 0x09
	opEXP        = 0x0a
	opSIGNEXTEND = 0x0b
	// 0x10 比较和位运算。Osaka 在这里多了 CLZ。
	opLT     = 0x10
	opGT     = 0x11
	opSLT    = 0x12
	opSGT    = 0x13
	opEQ     = 0x14
	opISZERO = 0x15
	opAND    = 0x16
	opOR     = 0x17
	opXOR    = 0x18
	opNOT    = 0x19
	opBYTE   = 0x1a
	opSHL    = 0x1b
	opSHR    = 0x1c
	opSAR    = 0x1d
	opCLZ    = 0x1e
	// 0x20 哈希。
	opKECCAK256 = 0x20
	// 0x30 调用上下文和代码。
	opADDRESS        = 0x30
	opBALANCE        = 0x31
	opORIGIN         = 0x32
	opCALLER         = 0x33
	opCALLVALUE      = 0x34
	opCALLDATALOAD   = 0x35
	opCALLDATASIZE   = 0x36
	opCALLDATACOPY   = 0x37
	opCODESIZE       = 0x38
	opCODECOPY       = 0x39
	opGASPRICE       = 0x3a
	opEXTCODESIZE    = 0x3b
	opEXTCODECOPY    = 0x3c
	opRETURNDATASIZE = 0x3d
	opRETURNDATACOPY = 0x3e
	opEXTCODEHASH    = 0x3f
	// 0x40 区块。0x44 在合并后是 PREVRANDAO。这条链没有随机数、base fee 和 blob，读出来是 0。
	opBLOCKHASH   = 0x40
	opCOINBASE    = 0x41
	opTIMESTAMP   = 0x42
	opNUMBER      = 0x43
	opPREVRANDAO  = 0x44
	opGASLIMIT    = 0x45
	opCHAINID     = 0x46
	opSELFBALANCE = 0x47
	opBASEFEE     = 0x48
	opBLOBHASH    = 0x49
	opBLOBBASEFEE = 0x4a
	// 0x50 栈、内存、存储和跳转。PUSH0、TLOAD、TSTORE、MCOPY 是后来的硬分叉加上的。
	opPOP      = 0x50
	opMLOAD    = 0x51
	opMSTORE   = 0x52
	opMSTORE8  = 0x53
	opSLOAD    = 0x54
	opSSTORE   = 0x55
	opJUMP     = 0x56
	opJUMPI    = 0x57
	opPC       = 0x58
	opMSIZE    = 0x59
	opGAS      = 0x5a
	opJUMPDEST = 0x5b
	opTLOAD    = 0x5c
	opTSTORE   = 0x5d
	opMCOPY    = 0x5e
	opPUSH0    = 0x5f
	opPUSH1    = 0x60
	opPUSH32   = 0x7f
	opDUP1     = 0x80
	opDUP16    = 0x8f
	opSWAP1    = 0x90
	opSWAP16   = 0x9f
	// 0xa0 日志。LOG0 没有 topic，LOG4 有四个。
	opLOG0 = 0xa0
	opLOG4 = 0xa4
	// 0xf0 调用和创建。EOF 的操作码不在 Osaka 的传统执行里，遇到就停机。
	opCREATE       = 0xf0
	opCALL         = 0xf1
	opCALLCODE     = 0xf2
	opRETURN       = 0xf3
	opDELEGATECALL = 0xf4
	opCREATE2      = 0xf5
	opSTATICCALL   = 0xfa
	opREVERT       = 0xfd
	opINVALID      = 0xfe
	opSELFDESTRUCT = 0xff

	// 执行这些操作要收的 gas。算术、比较、压栈和环境读取都是 gasBase，不跟 Osaka 的 gas 表。
	gasBase         uint64 = 3    // 加减、比较、压栈和读调用信息
	gasJump         uint64 = 8    // JUMP
	gasJumpi        uint64 = 10   // JUMPI
	gasJumpdest     uint64 = 1    // 落在 JUMPDEST
	gasSLoad        uint64 = 100  // 读存储
	gasSStore       uint64 = 5000 // 写存储
	gasTransient    uint64 = 100  // TLOAD 和 TSTORE
	gasKeccak       uint64 = 30   // KECCAK256 的底价
	gasKeccakWord   uint64 = 6    // KECCAK256 每个 32 字节字
	gasCall         uint64 = 100  // CALL 本身
	gasCallValue    uint64 = 9000 // CALL 还附带了金额，从本帧扣除
	gasCallStipend  uint64 = 2300 // 带金额的 CALL / CALLCODE 额外给子调用，不从本帧扣除
	gasCreate       uint64 = 32000
	gasSelfdestruct uint64 = 5000
	gasLog          uint64 = 375 // 一条日志
	gasLogTopic     uint64 = 375 // 日志里每个 topic
	gasLogData      uint64 = 8   // 日志数据的每个字节
	gasMemory       uint64 = 3   // 内存每个 32 字节字的线性单价
	memoryQuadDiv   uint64 = 512

	// 执行时不能超过的上限。
	maxCodeSize   = 24576           // 部署后的合约代码最多这么多字节
	maxInitCode   = maxCodeSize * 2 // 创建时的初始代码最多这么多字节
	maxStack      = 1024            // 栈最多这么多项
	maxMemorySize = 0x1FFFFFFFE0    // 再大的话，字数的平方放不进 uint64
	maxDepth      = 1024            // CALL 和 CREATE 最多嵌套这么层
	maxSteps      = 100000          // 一条指令算一步，防止跳转死循环
)

// execKind 是一帧执行的结果种类。只有成功、回退、停机三种。
type execKind int

const (
	// execOK 执行成功，这一帧改过的状态保留。
	execOK execKind = iota
	// execRevert 是回退：合约自己执行了 REVERT，主动宣布这次失败。
	// 这一帧改过的状态丢掉，日志不留。gas 只扣到 REVERT 这一条，剩下的退回。
	execRevert
	// execHalt 是停机：虚拟机自己中止，不是合约主动回退。
	// 原因包括 gas 用尽、非法操作码、栈错误、跳转步数耗尽，以及部署代码过长或部署 gas 不够。
	// 状态和日志同样丢掉。和回退不同的是，这一帧剩下的 gas 全部扣掉，不退。
	execHalt
)

var (
	errStop   = errors.New("stop")
	errRevert = errors.New("revert")
	errHalt   = errors.New("halt")
)

// CreateAddress 是合约地址：Keccak-256(RLP(sender, nonce)) 的后 20 字节。nonce 用创建前的值。
func CreateAddress(sender Address, nonce uint64) Address {
	sum := keccak(rlpList(rlpString(sender[:]), rlpUint(nonce)))
	var addr Address
	copy(addr[:], sum[12:])
	return addr
}

// machine 是一次调用的栈、内存和程序计数器。steps 在嵌套调用之间共用，防止跳转死循环。
type machine struct {
	state      *State       // 这一帧正在改的状态。内层 CALL 失败时，调用方换回调用前的快照。
	contract   Address      // 正在执行的合约。ADDRESS 读它。
	caller     Address      // 谁发起了这一帧。CALLER 读它。
	value      Uint256      // 这次调用附带的金额。CALLVALUE 读它。
	input      []byte       // 调用数据。创建合约时是 nil，初始代码放在 code 里。
	code       []byte       // 本帧要执行的字节码。
	dests      map[int]bool // 合法 JUMPDEST 的位置。PUSH 立即数里的同名字节不算。
	pc         int          // 下一条要读的字节。JUMP 会把它改掉。
	gas        uint64       // 这一帧还剩的 gas。
	stack      []Hash       // 栈顶是最后一项，每项 32 字节。
	mem        []byte       // 本帧内存。用到更远处时按 32 字节对齐扩张。
	memGas     uint64       // 已经为当前内存付过的 gas。再次扩张只收增量。
	logs       []Log        // 本帧成功时记下的日志。回退或停机时 runCode 不交出去。
	ret        []byte       // RETURN 和 REVERT 写入。STOP 不碰它。REVERT 的这段交给调用方做返回数据，不作为成功返回值。
	returnData []byte       // 最近一次子调用的返回数据。成功和回退都留下，停机清掉。RETURNDATACOPY 读它。
	depth      int          // 调用深度。交易入口是 1，每层 CALL 加 1。
	steps      *int         // 剩余步数。嵌套 CALL 共用这一个指针。
}

// runCode 从程序开头跑到停机、回退或代码结束。内层失败由调用方决定要不要恢复状态。
// value 是这笔调用附带的金额。区块高度和时间在状态的这一拍里，内层调用看到的是同一块。
// 四个返回值依次是返回数据、日志、剩余 gas 和结果种类。
// 返回数据在 RETURN 时是成功的返回值，在 REVERT 时是回退数据。代码自己跑完或停机时是 nil。
// 日志是这一帧成功时记下的；REVERT 或停机时丢掉，返回 nil。
// 剩余 gas 是停下来时还剩的；停机（execHalt）时是 0，这一帧剩下的 gas 全部扣掉。
// 结果种类是 execOK、execRevert 或 execHalt。
func runCode(st *State, contract, caller Address, value Uint256, input, code []byte, gas uint64, depth int, steps *int) ([]byte, []Log, uint64, execKind) {
	m := &machine{
		state: st, contract: contract, caller: caller, value: value,
		input: input, code: code, dests: jumpDests(code),
		gas: gas, depth: depth, steps: steps,
	}
	// pc 走到代码末尾，或某条指令用 STOP / REVERT 停下来，这一帧才结束。
	// 经典 EVM 也是每一帧从 PC = 0 开始
	for m.pc < len(m.code) {
		// 步数由外层交易创建，嵌套 CALL 一起扣。跳转把步数耗尽就停机，避免死循环。
		if *m.steps <= 0 {
			return nil, nil, 0, execHalt
		}
		*m.steps--
		op := m.code[m.pc]
		m.pc++
		err := m.exec(op)
		// STOP 和 RETURN 都返回 errStop，这一帧算成功。返回数据在 m.ret 里：
		// RETURN 事先从内存写入；STOP 没写过，所以是 nil。日志和剩余 gas 都留下。
		if errors.Is(err, errStop) {
			return m.ret, m.logs, m.gas, execOK
		}
		// REVERT 把内存写入 m.ret。这段是回退数据，不是成功的返回值。
		// 调用方把它留作 RETURNDATACOPY 的来源。日志不交出去。剩余 gas 退回。状态恢复由调用方做。
		if errors.Is(err, errRevert) {
			return m.ret, nil, m.gas, execRevert
		}
		// 非法操作码、栈错误或 gas 不够：这一帧剩下的 gas 全部作废。
		if err != nil {
			return nil, nil, 0, execHalt
		}
	}
	// 字节码自己走完，没有 RETURN。没有返回数据，已经记下的日志仍算成功。
	return nil, m.logs, m.gas, execOK
}

// jumpDests 标出合法跳转点。PUSH 后面的立即数里即使出现 JUMPDEST 的字节，也不能当跳转目标。
// go-ethereum: https://github.com/ethereum/go-ethereum/blob/master/core/vm/analysis_legacy.go#L64
func jumpDests(code []byte) map[int]bool {
	dest := make(map[int]bool)
	for i := 0; i < len(code); i++ {
		op := code[i]
		if op == opJUMPDEST {
			dest[i] = true
		}
		// 0x60 到 0x7f 是 PUSH1 到 PUSH32，后面跟着 1 到 32 字节立即数。
		// 立即数里出现的 0x5b(opJUMPDEST) 不是指令，扫描时整段跳过。
		if op >= opPUSH1 && op <= 0x7f {
			// 下轮处理下标位置 index = op - opPUSH1 + 2
			// for 循环下轮开始的时候 index 会自增，所以此处 op - opPUSH1 + 1 即可
			i += int(op - opPUSH1 + 1)
		}
	}
	return dest
}

// exec 执行当前这一条操作码。不认识的操作码停机。
// 多数指令先经 charge 扣 gas，扣完再改栈、内存或存储。
// 看 exec 函数的时候，时刻记住 m.pc 在调用 exec 前已经自动 +1 了
func (m *machine) exec(op byte) error {
	switch op {
	case opSTOP:
		// 成功停机，不带返回数据。runCode 把空的 m.ret 交出去。
		return errStop
	case opADD:
		return m.binop(addWord)
	case opMUL:
		return m.binop(mulWord)
	case opSUB:
		return m.binop(subWord)
	case opDIV:
		return m.binop(divWord)
	case opSDIV:
		return m.binop(sdivWord)
	case opMOD:
		return m.binop(modWord)
	case opSMOD:
		return m.binop(smodWord)
	case opADDMOD:
		return m.ternop(addModWord)
	case opMULMOD:
		return m.ternop(mulModWord)
	case opEXP:
		return m.binop(expWord)
	case opSIGNEXTEND:
		return m.binop(signExtendWord)
	case opLT:
		return m.binop(ltWord)
	case opGT:
		return m.binop(gtWord)
	case opSLT:
		return m.binop(sltWord)
	case opSGT:
		return m.binop(sgtWord)
	case opEQ:
		return m.binop(eqWord)
	case opISZERO:
		return m.opIsZero()
	case opAND:
		return m.binop(andWord)
	case opOR:
		return m.binop(orWord)
	case opXOR:
		return m.binop(xorWord)
	case opNOT:
		return m.unop(notWord)
	case opBYTE:
		return m.binop(byteWord)
	case opSHL:
		return m.binop(shlWord)
	case opSHR:
		return m.binop(shrWord)
	case opSAR:
		return m.binop(sarWord)
	case opCLZ:
		return m.unop(clzWord)
	case opKECCAK256:
		return m.opKeccak()
	case opADDRESS:
		return m.charge(gasBase, func() error { return m.push(hashFromAddress(m.contract)) })
	case opBALANCE:
		return m.opBalance()
	case opORIGIN:
		return m.charge(gasBase, func() error { return m.push(hashFromAddress(m.state.origin)) })
	case opCALLER:
		return m.charge(gasBase, func() error { return m.push(hashFromAddress(m.caller)) })
	case opCALLVALUE:
		return m.charge(gasBase, func() error { return m.push(Hash(m.value)) })
	case opCALLDATALOAD:
		return m.charge(gasBase, m.opCallDataLoad)
	case opCALLDATASIZE:
		return m.charge(gasBase, func() error {
			return m.push(hashFromUint64(uint64(len(m.input))))
		})
	case opCALLDATACOPY:
		return m.opCallDataCopy()
	case opCODESIZE:
		return m.charge(gasBase, func() error { return m.push(hashFromUint64(uint64(len(m.code)))) })
	case opCODECOPY:
		return m.charge(gasBase, m.opCodeCopy)
	case opGASPRICE:
		return m.charge(gasBase, func() error { return m.push(hashFromUint64(m.state.gasPrice)) })
	case opEXTCODESIZE:
		return m.opExtCodeSize()
	case opEXTCODECOPY:
		return m.opExtCodeCopy()
	case opRETURNDATASIZE:
		return m.charge(gasBase, func() error { return m.push(hashFromUint64(uint64(len(m.returnData)))) })
	case opRETURNDATACOPY:
		return m.opReturnDataCopy()
	case opEXTCODEHASH:
		return m.opExtCodeHash()
	case opBLOCKHASH:
		return m.opBlockHash()
	case opCOINBASE:
		return m.charge(gasBase, func() error { return m.push(hashFromAddress(m.state.coinbase)) })
	case opTIMESTAMP:
		return m.charge(gasBase, func() error { return m.push(hashFromUint64(m.state.blockTime)) })
	case opNUMBER:
		return m.charge(gasBase, func() error { return m.push(hashFromUint64(m.state.blockNumber)) })
	case opPREVRANDAO, opBASEFEE, opBLOBBASEFEE:
		// 这条链没有随机数、base fee 和 blob。操作码在，读出来是 0。
		return m.charge(gasBase, func() error { return m.push(Hash{}) })
	case opGASLIMIT:
		return m.charge(gasBase, func() error { return m.push(hashFromUint64(BlockGasLimit)) })
	case opCHAINID:
		return m.charge(gasBase, func() error { return m.push(hashFromUint64(ChainID)) })
	case opSELFBALANCE:
		return m.opSelfBalance()
	case opBLOBHASH:
		return m.opBlobHash()
	case opPOP:
		return m.opPop()
	case opMLOAD:
		return m.charge(gasBase, m.opMload)
	case opMSTORE:
		return m.charge(gasBase, m.opMstore)
	case opMSTORE8:
		return m.opMstore8()
	case opSLOAD:
		return m.charge(gasSLoad, m.opSload)
	case opSSTORE:
		return m.charge(gasSStore, m.opSstore)
	case opJUMP:
		return m.charge(gasJump, func() error { return m.jump(true) })
	case opJUMPI:
		return m.charge(gasJumpi, m.opJumpi)
	case opPC:
		// exec 调用前 pc 已经指向下一条，所以这条指令自己的位置是 pc-1。
		return m.charge(gasBase, func() error { return m.push(hashFromUint64(uint64(m.pc - 1))) })
	case opMSIZE:
		return m.charge(gasBase, func() error { return m.push(hashFromUint64(uint64(len(m.mem)))) })
	case opGAS:
		return m.charge(gasBase, func() error { return m.push(hashFromUint64(m.gas)) })
	case opJUMPDEST:
		return m.charge(gasJumpdest, func() error { return nil })
	case opTLOAD:
		return m.opTload()
	case opTSTORE:
		return m.opTstore()
	case opMCOPY:
		return m.opMcopy()
	case opPUSH0:
		return m.charge(gasBase, func() error { return m.push(Hash{}) })
	case opCREATE:
		return m.opCreate(false)
	case opCALL:
		return m.opCall()
	case opCALLCODE:
		return m.opCallCode()
	case opRETURN:
		// 先把内存写入 m.ret，再返回 errStop。runCode 把 m.ret 作为返回数据交出去。
		return m.opReturn(errStop)
	case opDELEGATECALL:
		return m.opDelegateCall()
	case opCREATE2:
		return m.opCreate(true)
	case opSTATICCALL:
		return m.opStaticCall()
	case opREVERT:
		// 同样写入 m.ret，但返回 errRevert。runCode 把这段交给调用方当作回退数据。
		return m.opReturn(errRevert)
	case opINVALID:
		return errHalt
	case opSELFDESTRUCT:
		return m.opSelfdestruct()
	default:
		switch {
		case op >= opPUSH1 && op <= opPUSH32:
			return m.charge(gasBase, func() error { return m.opPush(int(op - opPUSH1 + 1)) })
		case op >= opDUP1 && op <= opDUP16:
			return m.opDup(int(op - opDUP1 + 1))
		case op >= opSWAP1 && op <= opSWAP16:
			return m.opSwap(int(op - opSWAP1 + 1))
		case op >= opLOG0 && op <= opLOG4:
			return m.opLog(int(op - opLOG0))
		default:
			// EOF 和还没进 Osaka 的操作码走这里，整帧停机。
			return errHalt
		}
	}
}

// charge 先扣 cost。gas 不够就停机，fn 不会执行。
// fn 里面再失败时，这次已经扣掉的 gas 不退。停机路径还会把整帧剩余 gas 清掉。
func (m *machine) charge(cost uint64, fn func() error) error {
	if m.gas < cost {
		return errHalt
	}
	m.gas -= cost
	return fn()
}

// opIsZero 看栈顶。全 0 压入 1，否则压入 0。JUMPI 用这个 0 或 1 决定跳不跳。
func (m *machine) opIsZero() error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < 1 {
			return errHalt
		}
		if m.pop() == (Hash{}) {
			return m.push(hashFromUint64(1))
		}
		return m.push(Hash{})
	})
}

// unop 弹出栈顶，把结果压回去。
func (m *machine) unop(fn func(a Hash) Hash) error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < 1 {
			return errHalt
		}
		return m.push(fn(m.pop()))
	})
}

// binop 先弹出的 a 是栈顶，后弹出的 b 是下一字。ADD 算 a+b，SUB 算 a-b。
func (m *machine) binop(fn func(a, b Hash) Hash) error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < 2 {
			return errHalt
		}
		a := m.pop()
		b := m.pop()
		return m.push(fn(a, b))
	})
}

// ternop 先弹出的 a 是栈顶，然后是 b，最后是 c。ADDMOD 算 (a+b)%c。
func (m *machine) ternop(fn func(a, b, c Hash) Hash) error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < 3 {
			return errHalt
		}
		a := m.pop()
		b := m.pop()
		c := m.pop()
		return m.push(fn(a, b, c))
	})
}

// opPush 读操作码后面的 n 字节，右对齐放进 32 字节的字，左边补 0。
// 代码在立即数中间结束时，缺的字节保持 0。程序计数器仍往前移 n，外层循环随后结束。
func (m *machine) opPush(n int) error {
	raw := make([]byte, n)
	if m.pc < len(m.code) {
		end := m.pc + n
		if end > len(m.code) {
			end = len(m.code)
		}
		copy(raw, m.code[m.pc:end])
	}
	m.pc += n
	var word Hash
	copy(word[32-n:], raw)
	return m.push(word)
}

// opCallDataLoad 从调用数据的偏移处读 32 字节压栈。超出 input 的位置填 0。
// at >= off 挡住偏移加下标时的 uint64 回绕，回绕到的位置也填 0。
func (m *machine) opCallDataLoad() error {
	off, ok := m.popUint64()
	if !ok {
		return errHalt
	}
	var word Hash
	for i := 0; i < 32; i++ {
		at := off + uint64(i)
		if at < uint64(len(m.input)) && at >= off {
			word[i] = m.input[at]
		}
	}
	return m.push(word)
}

// opCodeCopy 把本帧代码从 off 起的 size 字节写到内存 dest。代码不够的位置写 0。
func (m *machine) opCodeCopy() error {
	dest, ok1 := m.popUint64()
	off, ok2 := m.popUint64()
	size, ok3 := m.popUint64()
	if !ok1 || !ok2 || !ok3 {
		return errHalt
	}
	if err := m.grow(dest, size); err != nil {
		return err
	}
	for i := uint64(0); i < size; i++ {
		var b byte
		at := off + i
		if at >= off && at < uint64(len(m.code)) {
			b = m.code[at]
		}
		m.mem[dest+i] = b
	}
	return nil
}

// opMload 从内存读 32 字节压栈。这段还不在内存里时先扩，并按新增字节收费。
func (m *machine) opMload() error {
	off, ok := m.popUint64()
	if !ok {
		return errHalt
	}
	if err := m.grow(off, 32); err != nil {
		return err
	}
	var word Hash
	copy(word[:], m.mem[off:off+32])
	return m.push(word)
}

// opMstore 把一个 32 字节字写进内存。先弹出偏移，再弹出要写的字。
func (m *machine) opMstore() error {
	off, ok := m.popUint64()
	if !ok {
		return errHalt
	}
	val := m.pop()
	if err := m.grow(off, 32); err != nil {
		return err
	}
	copy(m.mem[off:], val[:])
	return nil
}

// opSload 读本合约的存储。键是栈顶那个 32 字节字，没写过的槽读出来是 0。
func (m *machine) opSload() error {
	if len(m.stack) < 1 {
		return errHalt
	}
	key := m.pop()
	return m.push(m.state.GetStorage(m.contract, key))
}

// opSstore 写本合约的存储。先弹出键，再弹出值。写成全 0 的槽由状态层删掉。
// 只读帧里写存储会停机。gas 已经在外面扣过。
func (m *machine) opSstore() error {
	if m.state.readOnly {
		return errHalt
	}
	if len(m.stack) < 2 {
		return errHalt
	}
	key := m.pop()
	val := m.pop()
	m.state.SetStorage(m.contract, key, val)
	return nil
}

// opJumpi 先弹出跳转目标，再弹出条件。条件是全 0 就顺序执行下一条。
// 条件不是 0 时把目标放回栈顶，jump 再弹出来，并检查它是不是 JUMPDEST。
func (m *machine) opJumpi() error {
	dest := m.pop()
	cond := m.pop()
	if cond == (Hash{}) {
		return nil
	}
	m.stack = append(m.stack, dest)
	return m.jump(true)
}

// jump 把程序计数器改到栈顶给出的字节位置。must 为假时留在原地。
// 目标越界，或不是 jumpDests 标出的 JUMPDEST，都停机。
func (m *machine) jump(must bool) error {
	if !must {
		return nil
	}
	dest, ok := m.popUint64()
	if !ok || dest >= uint64(len(m.code)) || !m.dests[int(dest)] {
		return errHalt
	}
	m.pc = int(dest)
	return nil
}

// opLog 先弹出数据在内存中的偏移和长度，再弹出 topics 个主题。
// 费用是一笔日志的底价，加上每个 topic，再加上数据的每个字节。乘积放不下 uint64 就停机。
// 只读帧里打日志会停机。
func (m *machine) opLog(topics int) error {
	if m.state.readOnly {
		return errHalt
	}
	off, ok1 := m.popUint64()
	size, ok2 := m.popUint64()
	if !ok1 || !ok2 || len(m.stack) < topics {
		return errHalt
	}
	cost := gasLog + gasLogTopic*uint64(topics)
	// 数据按字节加价。长度乘单价放不下 uint64 时，先停机，避免费用被截断。
	if size > (^uint64(0))/gasLogData || cost > ^uint64(0)-size*gasLogData {
		return errHalt
	}
	cost += size * gasLogData
	if m.gas < cost {
		return errHalt
	}
	m.gas -= cost
	if err := m.grow(off, size); err != nil {
		return err
	}
	lg := Log{Address: m.contract, Data: append([]byte(nil), m.mem[off:off+size]...)}
	for i := 0; i < topics; i++ {
		lg.Topics = append(lg.Topics, m.pop())
	}
	m.logs = append(m.logs, lg)
	return nil
}

// opReturn 从内存切出一段写入 m.ret，然后用 done 结束这一帧。
// RETURN 传入 errStop：runCode 把 m.ret 作为第一个返回值交出去，创建合约时这段会变成代码。
// REVERT 传入 errRevert：runCode 丢掉 m.ret，只退剩余 gas。
func (m *machine) opReturn(done error) error {
	off, ok1 := m.popUint64()
	size, ok2 := m.popUint64()
	if !ok1 || !ok2 {
		return errHalt
	}
	if err := m.grow(off, size); err != nil {
		return err
	}
	// 返回数据只在这里产生。STOP 和代码自然结束都不会走到这，所以是空。
	m.ret = append([]byte(nil), m.mem[off:off+size]...)
	return done
}

// opCall 弹出 gas、地址、金额和输入输出区间。语义在 execCall。
func (m *machine) opCall() error { return m.execCall(modeCall) }

// opCallCode 用对方的代码，但存储、ADDRESS 和 CALLER 仍是当前合约，并按栈上的金额转账。
func (m *machine) opCallCode() error { return m.execCall(modeCallCode) }

// opDelegateCall 用对方的代码改自己的存储。CALLER 和 CALLVALUE 保持当前这一帧，不转账。
func (m *machine) opDelegateCall() error { return m.execCall(modeDelegate) }

// opStaticCall 只读调用。内层写存储、打日志、创建或带金额调用都会停机，外层仍可继续。
func (m *machine) opStaticCall() error { return m.execCall(modeStatic) }

// grow 把内存扩到能盖住这段区间。长度向上取整到 32 字节，MSIZE 就是对齐后的字节数。
// 费用是 words^2/512 + 3*words，只收比上次多出来的部分。长度为 0 不扩张。
func (m *machine) grow(off, size uint64) error {
	if size == 0 {
		return nil
	}
	// 超过这个长度时，字数的平方放不进 uint64。
	if off > maxMemorySize || size > maxMemorySize-off {
		return errHalt
	}
	end := off + size
	words := (end + 31) / 32
	aligned := words * 32
	if aligned <= uint64(len(m.mem)) {
		return nil
	}
	// 新的总费用。maxMemorySize 保证平方和加法都不会溢出。
	total := words*gasMemory + (words*words)/memoryQuadDiv
	fee := total - m.memGas
	if m.gas < fee {
		return errHalt
	}
	m.gas -= fee
	m.memGas = total
	next := make([]byte, aligned)
	copy(next, m.mem)
	m.mem = next
	return nil
}

// push 把一个 32 字节字放到栈顶。栈已经有 maxStack 项就停机。
func (m *machine) push(w Hash) error {
	if len(m.stack) >= maxStack {
		return errHalt
	}
	m.stack = append(m.stack, w)
	return nil
}

// pop 取走栈顶。调用方要先确认栈里有字，这里不再检查。
func (m *machine) pop() Hash {
	w := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return w
}

// popUint64 弹出栈顶并收成 uint64。栈是空的，或高 24 字节不是 0，都算失败。
func (m *machine) popUint64() (uint64, bool) {
	if len(m.stack) == 0 {
		return 0, false
	}
	return hashUint64(m.pop())
}

// hashUint64 只接受高 24 字节为 0 的字。放不下 uint64 的金额或长度算执行失败。
func hashUint64(h Hash) (uint64, bool) {
	for _, b := range h[:24] {
		if b != 0 {
			return 0, false
		}
	}
	return binary.BigEndian.Uint64(h[24:]), true
}

// hashFromUint64 把整数放进字的最后 8 字节，前面补 0。
func hashFromUint64(n uint64) Hash {
	var h Hash
	binary.BigEndian.PutUint64(h[24:], n)
	return h
}

// addressFromHash 取字的后 20 字节当地址。前 12 字节不参与。
func addressFromHash(h Hash) Address {
	var a Address
	copy(a[:], h[12:])
	return a
}

// hashFromAddress 把 20 字节地址放进字的末尾，前面补 0。ADDRESS 和 CALLER 压栈时用它。
func hashFromAddress(a Address) Hash {
	var h Hash
	copy(h[12:], a[:])
	return h
}

// 加减都在 2^256 上取模。SUB 是栈顶减去下一个。
// LT、EQ 比较的是整字。栈顶是左操作数，结果是 1 或 0。
func addWord(a, b Hash) Hash {
	x := new(big.Int).SetBytes(a[:])
	y := new(big.Int).SetBytes(b[:])
	return hashMod(x.Add(x, y))
}

func subWord(a, b Hash) Hash {
	x := new(big.Int).SetBytes(a[:])
	y := new(big.Int).SetBytes(b[:])
	return hashMod(x.Sub(x, y))
}

func ltWord(a, b Hash) Hash {
	for i := 0; i < len(a); i++ {
		if a[i] < b[i] {
			return hashFromUint64(1)
		}
		if a[i] > b[i] {
			return Hash{}
		}
	}
	return Hash{}
}

func eqWord(a, b Hash) Hash {
	if a == b {
		return hashFromUint64(1)
	}
	return Hash{}
}

// hashMod 将任意大小的有符号大整数 x，归一化为 EVM 标准的 32 字节（256位）无符号大端序 Hash 格式
// 核心处理逻辑：
//  1. 对 2^256 取模，将数值收敛到 [0, 2^256 - 1] 区间，同时将负数转换为 EVM 等价的同余正数
//  2. 将结果右对齐填充到 32 字节数组中，高位自动补零，符合 uint256 大端序存储规范
func hashMod(x *big.Int) Hash {
	// 计算模数 2^256：EVM 字长为 256 位，所有运算天然模 2^256
	mod := new(big.Int).Lsh(big.NewInt(1), 256)

	// x = x mod 2^256
	// - 正数：直接取模，截断超出 256 位的高位，模拟 EVM 溢出行为
	// - 负数：Go big.Int.Mod 结果与模数同号，自动转为 [0, 2^256-1] 范围内的同余正数
	//   例如：-1 mod 2^256 = 2^256 - 1，与 EVM 中 0-1 的计算结果完全一致
	x.Mod(x, mod)

	// 将大整数转为字节切片：大端序、最短表示（无前导零）
	// 例如：数值 1 得到 []byte{0x01}，长度为 1
	raw := x.Bytes()

	// 声明 32 字节的 Hash 数组，初始值全为 0
	var h Hash

	// 将字节切片右对齐拷贝到 32 字节数组末尾
	// 高位（左侧）自动保留初始的 0，补齐前导零，得到标准 32 字节大端 uint256 格式
	copy(h[32-len(raw):], raw)

	return h
}
