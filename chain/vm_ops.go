package chain

import "errors"

// callMode 区分四种调用。栈上有没有金额、代码在谁的存储里跑，都由它决定。
type callMode int

const (
	modeCall callMode = iota
	modeCallCode
	modeDelegate
	modeStatic
)

func (mode callMode) stackN() int {
	if mode == modeDelegate || mode == modeStatic {
		return 6
	}
	return 7
}

// execCall 执行 CALL、CALLCODE、DELEGATECALL 或 STATICCALL。
// 转出的 gas 不超过本帧剩余的 63/64。内层回退或停机只恢复这次调用前的状态。
// 成功和回退都会把返回数据写进输出区，并留下给 RETURNDATACOPY。停机不写输出区。
func (m *machine) execCall(mode callMode) error {
	n := mode.stackN()
	// 嵌套到 maxDepth 时这次调用失败：参数弹出，压入 0，本帧继续。
	if m.depth >= maxDepth {
		return m.popFail(n)
	}
	if len(m.stack) < n {
		return errHalt
	}
	gasReq, okG := m.popUint64()
	to := addressFromHash(m.pop())
	value := m.value
	transfer := false
	switch mode {
	case modeCall, modeCallCode:
		value = Uint256(m.pop())
		transfer = true
	case modeStatic:
		value = Uint256{}
	}
	inOff, ok1 := m.popUint64()
	inSize, ok2 := m.popUint64()
	retOff, ok3 := m.popUint64()
	retSize, ok4 := m.popUint64()
	if !okG || !ok1 || !ok2 || !ok3 || !ok4 {
		return errHalt
	}
	// 只读帧里再转出金额，整帧停机，而不是压入 0。
	if transfer && !value.IsZero() && m.state.readOnly {
		return errHalt
	}
	cost := gasCall
	if transfer && !value.IsZero() {
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
	var input []byte
	if inSize > 0 {
		input = append([]byte(nil), m.mem[inOff:inOff+inSize]...)
	}

	contract := to
	caller := m.contract
	codeAt := to
	switch mode {
	case modeCallCode:
		// 代码来自对方，存储和 ADDRESS 仍是自己。CALLER 是自己。
		contract = m.contract
		caller = m.contract
	case modeDelegate:
		// 代码来自对方，存储和 ADDRESS 仍是自己。CALLER 和金额保持当前帧。
		contract = m.contract
		caller = m.caller
	}

	available := m.gas - m.gas/64
	forward := available
	if gasReq < forward {
		forward = gasReq
	}
	m.gas -= forward
	// 带金额的 CALL 和 CALLCODE 再给子调用 2300。这笔不从本帧扣。
	// 子调用没用完的部分会退回来，所以本帧的 gas 最多能多出 2300。
	childGas := forward
	if transfer && !value.IsZero() {
		if childGas > ^uint64(0)-gasCallStipend {
			return errHalt
		}
		childGas += gasCallStipend
	}

	prevOnly := m.state.readOnly
	if mode == modeStatic {
		m.state.readOnly = true
	}
	defer func() { m.state.readOnly = prevOnly }()

	snap := m.state.Copy()
	if transfer && !value.IsZero() {
		if err := m.state.transfer(m.contract, to, value); err != nil {
			// 转账没做成。转出的 gas 退回。余额溢出则整帧停机，余额不够则压入 0。
			m.gas += forward
			m.returnData = nil
			if errors.Is(err, ErrBalanceOverflow) {
				return err
			}
			return m.push(Hash{})
		}
	}
	// 预编译按目标地址查，不看这个账户有没有代码。四种调用都走这张表。
	// 签名不合法时调用仍成功，返回数据是空的。gas 不够则恢复这次调用前的状态，栈上压 0。
	if ret, left, kind, hit := runPrecompile(to, input, childGas); hit {
		m.gas += left
		if kind != execOK {
			m.state.restore(snap)
			m.returnData = nil
			return m.push(Hash{})
		}
		m.returnData = append([]byte(nil), ret...)
		if err := m.writeCallReturn(retOff, retSize, m.returnData); err != nil {
			return err
		}
		return m.push(hashFromUint64(1))
	}
	code := m.state.GetCode(codeAt)
	if len(code) == 0 {
		m.gas += childGas
		m.returnData = nil
		return m.push(hashFromUint64(1))
	}
	ret, logs, left, kind := runCode(m.state, contract, caller, value, input, code, childGas, m.depth+1, m.steps)
	m.gas += left
	if kind != execOK {
		m.state.restore(snap)
		if kind == execRevert {
			m.returnData = append([]byte(nil), ret...)
			if err := m.writeCallReturn(retOff, retSize, m.returnData); err != nil {
				return err
			}
		} else {
			m.returnData = nil
		}
		return m.push(Hash{})
	}
	m.returnData = append([]byte(nil), ret...)
	m.logs = append(m.logs, logs...)
	if err := m.writeCallReturn(retOff, retSize, m.returnData); err != nil {
		return err
	}
	return m.push(hashFromUint64(1))
}

// popFail 在调用深度用尽时弹出 n 个参数并压入 0。栈不够则停机。
func (m *machine) popFail(n int) error {
	if len(m.stack) < n {
		return errHalt
	}
	for i := 0; i < n; i++ {
		m.pop()
	}
	return m.push(Hash{})
}

// writeCallReturn 把返回数据写进输出区。短了的部分清成 0，长出去的丢掉。
func (m *machine) writeCallReturn(off, size uint64, ret []byte) error {
	if size == 0 {
		return nil
	}
	n := size
	if uint64(len(ret)) < n {
		n = uint64(len(ret))
	}
	copy(m.mem[off:off+size], make([]byte, size))
	copy(m.mem[off:], ret[:n])
	return nil
}

func (m *machine) opBalance() error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < 1 {
			return errHalt
		}
		bal, _ := m.state.GetBalanceNonce(addressFromHash(m.pop()))
		return m.push(Hash(bal))
	})
}

func (m *machine) opSelfBalance() error {
	return m.charge(gasBase, func() error {
		bal, _ := m.state.GetBalanceNonce(m.contract)
		return m.push(Hash(bal))
	})
}

func (m *machine) opExtCodeSize() error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < 1 {
			return errHalt
		}
		code := m.state.GetCode(addressFromHash(m.pop()))
		return m.push(hashFromUint64(uint64(len(code))))
	})
}

// opExtCodeHash 账户不存在时压入 0。账户在、但没有代码时，是空字节的 Keccak-256。
func (m *machine) opExtCodeHash() error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < 1 {
			return errHalt
		}
		addr := addressFromHash(m.pop())
		if _, ok := m.state.GetAccount(addr); !ok {
			return m.push(Hash{})
		}
		return m.push(codeHash(m.state.GetCode(addr)))
	})
}

func (m *machine) opBlockHash() error {
	return m.charge(gasBase, func() error {
		n, ok := m.popUint64()
		if !ok {
			return errHalt
		}
		return m.push(m.state.blockHash(n))
	})
}

// opBlobHash 这条链没有 blob 交易，任何下标都是 0。下标放不进 uint64 同样是 0，不停机。
func (m *machine) opBlobHash() error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < 1 {
			return errHalt
		}
		m.pop()
		return m.push(Hash{})
	})
}

func (m *machine) opPop() error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < 1 {
			return errHalt
		}
		m.pop()
		return nil
	})
}

func (m *machine) opDup(n int) error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < n {
			return errHalt
		}
		return m.push(m.stack[len(m.stack)-n])
	})
}

func (m *machine) opSwap(n int) error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < n+1 {
			return errHalt
		}
		top := len(m.stack) - 1
		m.stack[top], m.stack[top-n] = m.stack[top-n], m.stack[top]
		return nil
	})
}

func (m *machine) opMstore8() error {
	return m.charge(gasBase, func() error {
		off, ok := m.popUint64()
		if !ok || len(m.stack) < 1 {
			return errHalt
		}
		val := m.pop()
		if err := m.grow(off, 1); err != nil {
			return err
		}
		m.mem[off] = val[31]
		return nil
	})
}

func (m *machine) opMcopy() error {
	return m.charge(gasBase, func() error {
		dest, ok1 := m.popUint64()
		src, ok2 := m.popUint64()
		size, ok3 := m.popUint64()
		if !ok1 || !ok2 || !ok3 {
			return errHalt
		}
		if err := m.grow(dest, size); err != nil {
			return err
		}
		if err := m.grow(src, size); err != nil {
			return err
		}
		if size == 0 {
			return nil
		}
		buf := make([]byte, size)
		copy(buf, m.mem[src:src+size])
		copy(m.mem[dest:], buf)
		return nil
	})
}

func (m *machine) opCallDataCopy() error {
	return m.charge(gasBase, func() error {
		dest, ok1 := m.popUint64()
		off, ok2 := m.popUint64()
		size, ok3 := m.popUint64()
		if !ok1 || !ok2 || !ok3 {
			return errHalt
		}
		return m.copyToMem(dest, paddedSlice(m.input, off, size))
	})
}

func (m *machine) opExtCodeCopy() error {
	return m.charge(gasBase, func() error {
		if len(m.stack) < 4 {
			return errHalt
		}
		addr := addressFromHash(m.pop())
		dest, ok1 := m.popUint64()
		off, ok2 := m.popUint64()
		size, ok3 := m.popUint64()
		if !ok1 || !ok2 || !ok3 {
			return errHalt
		}
		return m.copyToMem(dest, paddedSlice(m.state.GetCode(addr), off, size))
	})
}

func (m *machine) opReturnDataCopy() error {
	return m.charge(gasBase, func() error {
		dest, ok1 := m.popUint64()
		off, ok2 := m.popUint64()
		size, ok3 := m.popUint64()
		if !ok1 || !ok2 || !ok3 {
			return errHalt
		}
		if off > uint64(len(m.returnData)) || (size > 0 && size > uint64(len(m.returnData))-off) {
			return errHalt
		}
		return m.copyToMem(dest, paddedSlice(m.returnData, off, size))
	})
}

func (m *machine) opKeccak() error {
	if len(m.stack) < 2 {
		return errHalt
	}
	off, ok1 := m.popUint64()
	size, ok2 := m.popUint64()
	if !ok1 || !ok2 {
		return errHalt
	}
	var words uint64
	if size > 0 {
		if size > ^uint64(0)-31 {
			return errHalt
		}
		words = (size + 31) / 32
		if gasKeccakWord != 0 && words > (^uint64(0)-gasKeccak)/gasKeccakWord {
			return errHalt
		}
	}
	return m.charge(gasKeccak+words*gasKeccakWord, func() error {
		var data []byte
		if size > 0 {
			if err := m.grow(off, size); err != nil {
				return err
			}
			data = m.mem[off : off+size]
		}
		return m.push(keccak(data))
	})
}

func (m *machine) opTload() error {
	return m.charge(gasTransient, func() error {
		if len(m.stack) < 1 {
			return errHalt
		}
		return m.push(m.state.GetTransient(m.contract, m.pop()))
	})
}

func (m *machine) opTstore() error {
	if m.state.readOnly {
		return errHalt
	}
	return m.charge(gasTransient, func() error {
		if len(m.stack) < 2 {
			return errHalt
		}
		key := m.pop()
		val := m.pop()
		m.state.SetTransient(m.contract, key, val)
		return nil
	})
}

// opCreate 执行 CREATE 或 CREATE2。失败时压入 0，本帧继续。
// 初始代码超过上限、或落在只读帧里，则整帧停机。
// 创建方的 nonce 一旦加上就不退，即使初始代码回退。碰撞或代码不合法时，转给初始代码的 gas 不退。
func (m *machine) opCreate(create2 bool) error {
	need := 3
	if create2 {
		need = 4
	}
	if m.state.readOnly {
		return errHalt
	}
	if len(m.stack) < need {
		return errHalt
	}
	value := Uint256(m.pop())
	off, ok1 := m.popUint64()
	size, ok2 := m.popUint64()
	var salt Hash
	if create2 {
		salt = m.pop()
	}
	if !ok1 || !ok2 {
		return errHalt
	}
	if size > maxInitCode {
		return errHalt
	}
	if m.depth >= maxDepth {
		return m.push(Hash{})
	}
	if err := m.grow(off, size); err != nil {
		return err
	}
	if m.gas < gasCreate {
		return errHalt
	}
	m.gas -= gasCreate
	var init []byte
	if size > 0 {
		init = append([]byte(nil), m.mem[off:off+size]...)
	}
	creator := m.state.GetAccountOrZero(m.contract)
	if creator.Nonce == ^uint64(0) || creator.Balance.Cmp(value) < 0 {
		m.returnData = nil
		return m.push(Hash{})
	}
	creator.Nonce++
	if creator.Address == (Address{}) {
		creator.Address = m.contract
	}
	m.state.SetAccount(creator)
	var addr Address
	if create2 {
		addr = CreateAddress2(m.contract, salt, init)
	} else {
		addr = CreateAddress(m.contract, creator.Nonce-1)
	}
	forward := m.gas - m.gas/64
	m.gas -= forward
	if accountExists(m.state, addr) {
		m.returnData = nil
		return m.push(Hash{})
	}
	snap := m.state.Copy()
	created := m.state.GetAccountOrZero(addr)
	created.Nonce = 1
	if created.Address == (Address{}) {
		created.Address = addr
	}
	m.state.SetAccount(created)
	m.state.markCreated(addr)
	if err := m.state.transfer(m.contract, addr, value); err != nil {
		m.state.restore(snap)
		m.gas += forward
		m.returnData = nil
		if errors.Is(err, ErrBalanceOverflow) {
			return err
		}
		return m.push(Hash{})
	}
	ret, left, kind := m.runInit(addr, value, init, forward)
	if kind != execOK || !deployable(ret, left) {
		m.state.restore(snap)
		if kind == execRevert {
			m.gas += left
			m.returnData = append([]byte(nil), ret...)
		} else {
			m.returnData = nil
		}
		return m.push(Hash{})
	}
	deposit := uint64(len(ret)) * CreateDataGas
	m.gas += left - deposit
	m.state.SetCode(addr, ret)
	m.returnData = nil
	return m.push(hashFromAddress(addr))
}

func (m *machine) runInit(addr Address, value Uint256, init []byte, gas uint64) ([]byte, uint64, execKind) {
	if len(init) == 0 {
		return nil, gas, execOK
	}
	ret, _, left, kind := runCode(m.state, addr, m.contract, value, nil, init, gas, m.depth+1, m.steps)
	return ret, left, kind
}

// deployable 判断初始代码的返回值能不能存成合约代码。
// 以 0xef 开头、超过长度上限，或剩下的 gas 不够按字节收费，都不能部署。这些失败不退还初始代码没用完的 gas。
func deployable(ret []byte, left uint64) bool {
	if len(ret) > 0 && ret[0] == 0xef {
		return false
	}
	if uint64(len(ret)) > maxCodeSize {
		return false
	}
	return left >= uint64(len(ret))*CreateDataGas
}

// accountExists 有 nonce 或有代码时，这个地址不能再部署。只有余额的账户可以。
func accountExists(s *State, addr Address) bool {
	if acct, ok := s.GetAccount(addr); ok && acct.Nonce > 0 {
		return true
	}
	return len(s.GetCode(addr)) > 0
}

// CreateAddress2 是 CREATE2 的地址：Keccak-256(0xff ++ sender ++ salt ++ Keccak-256(init)) 的后 20 字节。
func CreateAddress2(sender Address, salt Hash, initCode []byte) Address {
	buf := make([]byte, 1+20+32+32)
	buf[0] = 0xff
	copy(buf[1:21], sender[:])
	copy(buf[21:53], salt[:])
	sum := keccak(initCode)
	copy(buf[53:], sum[:])
	digest := keccak(buf)
	var addr Address
	copy(addr[:], digest[12:])
	return addr
}

func (m *machine) opSelfdestruct() error {
	if m.state.readOnly {
		return errHalt
	}
	return m.charge(gasSelfdestruct, func() error {
		if len(m.stack) < 1 {
			return errHalt
		}
		to := addressFromHash(m.pop())
		bal, _ := m.state.GetBalanceNonce(m.contract)
		// 本笔新建的合约，交易成功结束时会删掉。转给自己则把余额烧掉。
		// 更早的合约只转走余额，代码和存储留着。
		if m.state.created[m.contract] {
			if to != m.contract {
				if err := m.state.transfer(m.contract, to, bal); err != nil {
					return err
				}
			} else if !bal.IsZero() {
				acct := m.state.GetAccountOrZero(m.contract)
				acct.Balance = Uint256{}
				m.state.SetAccount(acct)
			}
			m.state.markDestructed(m.contract)
		} else if to != m.contract && !bal.IsZero() {
			if err := m.state.transfer(m.contract, to, bal); err != nil {
				return err
			}
		}
		return errStop
	})
}

func (m *machine) copyToMem(dest uint64, src []byte) error {
	if err := m.grow(dest, uint64(len(src))); err != nil {
		return err
	}
	copy(m.mem[dest:], src)
	return nil
}

// paddedSlice 从 src 的 off 起取 size 字节。越界的位置是 0。
func paddedSlice(src []byte, off, size uint64) []byte {
	out := make([]byte, size)
	for i := uint64(0); i < size; i++ {
		at := off + i
		if at < off || at >= uint64(len(src)) {
			continue
		}
		out[i] = src[at]
	}
	return out
}
