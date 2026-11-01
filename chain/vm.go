package chain

import (
	"encoding/binary"
	"errors"
	"math/big"
)

// 虚拟机只实现说明执行所需要的操作码。字长 32 字节，和以太坊一样。
// 余额和 gas 仍是 uint64，1 个币是 10^18 个最小单位。调用、回退和停机的结果由执行层决定要不要保留状态。
const (
	opSTOP         = 0x00
	opADD          = 0x01
	opSUB          = 0x03
	opCALLDATALOAD = 0x35
	opCALLDATASIZE = 0x36
	opCODECOPY     = 0x39
	opMLOAD        = 0x51
	opMSTORE       = 0x52
	opSLOAD        = 0x54
	opSSTORE       = 0x55
	opJUMP         = 0x56
	opJUMPI        = 0x57
	opJUMPDEST     = 0x5b
	opPUSH1        = 0x60
	opLOG0         = 0xa0
	opLOG1         = 0xa1
	opCALL         = 0xf1
	opRETURN       = 0xf3
	opREVERT       = 0xfd

	gasBase      uint64 = 3
	gasJump      uint64 = 8
	gasJumpi     uint64 = 10
	gasJumpdest  uint64 = 1
	gasSLoad     uint64 = 100
	gasSStore    uint64 = 5000
	gasCall      uint64 = 100
	gasCallValue uint64 = 9000
	gasLog       uint64 = 375
	gasLogTopic  uint64 = 375
	gasLogData   uint64 = 8
	gasMemory    uint64 = 3

	maxCodeSize = 24576
	maxStack    = 1024
	maxMemory   = 65536
	maxDepth    = 1024
	maxSteps    = 100000
)

// execOK 保留状态。execRevert 是 REVERT，退回状态，gas 只扣到停下来的地方。
// execHalt 是 gas 用尽、非法操作码或栈错误，这一帧的剩余 gas 全部扣掉。
const (
	execOK = iota
	execRevert
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
	state    *State
	contract Address
	caller   Address
	input    []byte
	code     []byte
	dests    map[int]bool
	pc       int
	gas      uint64
	stack    []Hash
	mem      []byte
	logs     []Log
	ret      []byte
	depth    int
	steps    *int
}

// runCode 从程序开头跑到停机、回退或代码结束，返回剩余 gas 和结果种类。内层失败由调用方决定要不要恢复状态。
func runCode(st *State, contract, caller Address, input, code []byte, gas uint64, depth int, steps *int) ([]byte, []Log, uint64, int) {
	m := &machine{
		state: st, contract: contract, caller: caller,
		input: input, code: code, dests: jumpDests(code),
		gas: gas, depth: depth, steps: steps,
	}
	for m.pc < len(m.code) {
		if *m.steps <= 0 {
			return nil, nil, 0, execHalt
		}
		*m.steps--
		op := m.code[m.pc]
		m.pc++
		err := m.exec(op)
		if errors.Is(err, errStop) {
			return m.ret, m.logs, m.gas, execOK
		}
		if errors.Is(err, errRevert) {
			return nil, nil, m.gas, execRevert
		}
		if err != nil {
			return nil, nil, 0, execHalt
		}
	}
	return nil, m.logs, m.gas, execOK
}

// jumpDests 标出合法跳转点。PUSH 后面的立即数里即使出现 JUMPDEST 的字节，也不能当跳转目标。
func jumpDests(code []byte) map[int]bool {
	dest := make(map[int]bool)
	for i := 0; i < len(code); i++ {
		op := code[i]
		if op == opJUMPDEST {
			dest[i] = true
		}
		if op >= opPUSH1 && op <= 0x7f {
			i += int(op - opPUSH1 + 1)
		}
	}
	return dest
}

func (m *machine) exec(op byte) error {
	switch {
	case op == opSTOP:
		return errStop
	case op == opADD:
		return m.binop(addWord)
	case op == opSUB:
		return m.binop(subWord)
	case op == opCALLDATASIZE:
		return m.charge(gasBase, func() error {
			return m.push(hashFromUint64(uint64(len(m.input))))
		})
	case op == opCALLDATALOAD:
		return m.charge(gasBase, m.opCallDataLoad)
	case op == opCODECOPY:
		return m.charge(gasBase, m.opCodeCopy)
	case op == opMLOAD:
		return m.charge(gasBase, m.opMload)
	case op == opMSTORE:
		return m.charge(gasBase, m.opMstore)
	case op == opSLOAD:
		return m.charge(gasSLoad, m.opSload)
	case op == opSSTORE:
		return m.charge(gasSStore, m.opSstore)
	case op == opJUMP:
		return m.charge(gasJump, func() error { return m.jump(true) })
	case op == opJUMPI:
		return m.charge(gasJumpi, m.opJumpi)
	case op == opJUMPDEST:
		return m.charge(gasJumpdest, func() error { return nil })
	case op >= opPUSH1 && op <= 0x7f:
		return m.charge(gasBase, func() error { return m.opPush(int(op - opPUSH1 + 1)) })
	case op == opLOG0:
		return m.opLog(0)
	case op == opLOG1:
		return m.opLog(1)
	case op == opCALL:
		return m.opCall()
	case op == opRETURN:
		return m.opReturn(errStop)
	case op == opREVERT:
		return m.opReturn(errRevert)
	default:
		return errHalt
	}
}

func (m *machine) charge(cost uint64, fn func() error) error {
	if m.gas < cost {
		return errHalt
	}
	m.gas -= cost
	return fn()
}

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

func (m *machine) opSload() error {
	key := m.pop()
	return m.push(m.state.Storage(m.contract, key))
}

func (m *machine) opSstore() error {
	key := m.pop()
	val := m.pop()
	m.state.SetStorage(m.contract, key, val)
	return nil
}

func (m *machine) opJumpi() error {
	dest := m.pop()
	cond := m.pop()
	if cond == (Hash{}) {
		return nil
	}
	m.stack = append(m.stack, dest)
	return m.jump(true)
}

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

func (m *machine) opLog(topics int) error {
	off, ok1 := m.popUint64()
	size, ok2 := m.popUint64()
	if !ok1 || !ok2 || len(m.stack) < topics {
		return errHalt
	}
	cost := gasLog + gasLogTopic*uint64(topics)
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

func (m *machine) opReturn(done error) error {
	off, ok1 := m.popUint64()
	size, ok2 := m.popUint64()
	if !ok1 || !ok2 {
		return errHalt
	}
	if err := m.grow(off, size); err != nil {
		return err
	}
	m.ret = append([]byte(nil), m.mem[off:off+size]...)
	return done
}

// opCall 弹出 gas、地址、金额和输入输出区间。转出的 gas 不超过本帧剩余的 63/64。
// 金额必须放进 uint64。被调用方没有代码时退回转出的 gas，并压入 1。
// 内层回退只恢复这次调用前的状态，外层可以继续成功。
func (m *machine) opCall() error {
	if len(m.stack) < 7 {
		return errHalt
	}
	if m.depth >= maxDepth {
		for i := 0; i < 7; i++ {
			m.pop()
		}
		return m.push(Hash{})
	}
	gasReq, okG := m.popUint64()
	to := addressFromHash(m.pop())
	value, okV := m.popUint64()
	inOff, ok1 := m.popUint64()
	inSize, ok2 := m.popUint64()
	retOff, ok3 := m.popUint64()
	retSize, ok4 := m.popUint64()
	if !okG || !okV || !ok1 || !ok2 || !ok3 || !ok4 {
		return errHalt
	}
	cost := gasCall
	if value > 0 {
		cost += gasCallValue
	}
	if m.gas < cost {
		return errHalt
	}
	m.gas -= cost
	if err := m.grow(inOff, inSize); err != nil {
		return err
	}
	if err := m.grow(retOff, retSize); err != nil {
		return err
	}
	input := append([]byte(nil), m.mem[inOff:inOff+inSize]...)
	available := m.gas - m.gas/64
	forward := available
	if gasReq < forward {
		forward = gasReq
	}
	m.gas -= forward
	snap := m.state.Copy()
	if err := m.state.transfer(m.contract, to, value); err != nil {
		m.gas += forward
		if errors.Is(err, ErrBalanceOverflow) {
			return err
		}
		return m.push(Hash{})
	}
	code := m.state.Code(to)
	if len(code) == 0 {
		m.gas += forward
		return m.push(hashFromUint64(1))
	}
	ret, logs, left, kind := runCode(m.state, to, m.contract, input, code, forward, m.depth+1, m.steps)
	m.gas += left
	if kind != execOK {
		m.state.restore(snap)
		return m.push(Hash{})
	}
	m.logs = append(m.logs, logs...)
	n := retSize
	if uint64(len(ret)) < n {
		n = uint64(len(ret))
	}
	copy(m.mem[retOff:retOff+retSize], make([]byte, retSize))
	copy(m.mem[retOff:], ret[:n])
	return m.push(hashFromUint64(1))
}

// grow 把内存扩到能盖住这段区间。每多一个字节收 gasMemory，上限是 maxMemory。
func (m *machine) grow(off, size uint64) error {
	if size == 0 {
		return nil
	}
	if off > maxMemory || size > maxMemory-off {
		return errHalt
	}
	end := off + size
	if end <= uint64(len(m.mem)) {
		return nil
	}
	growBy := end - uint64(len(m.mem))
	if growBy > m.gas/gasMemory {
		return errHalt
	}
	m.gas -= growBy * gasMemory
	next := make([]byte, end)
	copy(next, m.mem)
	m.mem = next
	return nil
}

func (m *machine) push(w Hash) error {
	if len(m.stack) >= maxStack {
		return errHalt
	}
	m.stack = append(m.stack, w)
	return nil
}

func (m *machine) pop() Hash {
	w := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return w
}

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

func hashFromUint64(n uint64) Hash {
	var h Hash
	binary.BigEndian.PutUint64(h[24:], n)
	return h
}

func addressFromHash(h Hash) Address {
	var a Address
	copy(a[:], h[12:])
	return a
}

// 加减都在 2^256 上取模。SUB 是栈顶减去下一个。
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

func hashMod(x *big.Int) Hash {
	mod := new(big.Int).Lsh(big.NewInt(1), 256)
	x.Mod(x, mod)
	raw := x.Bytes()
	var h Hash
	copy(h[32-len(raw):], raw)
	return h
}
