package chain

// 这个文件把一笔交易应用到状态上，并计算它的 gas。
// 转账、调用合约和创建合约都从 Apply 进入。起步 gas、data 加价和区块 gas 上限也定义在这里。
// 签名、chain id、nonce 或余额不过关时，状态保持原样，这笔不能入块。
// 回退或停机时仍返回回执：nonce 加 1，费用付给 coinbase，这次改过的状态丢掉。
// 回退只收已经用掉的 gas。停机把剩余 gas 也扣掉，按整个 gasLimit 收。

const (
	// IntrinsicGas 是一笔没有 data 的转账至少要付的 gas。
	IntrinsicGas uint64 = 21000
	// TxGasContractCreation 是创建合约的起步 gas，等于 21000 再加 32000。
	TxGasContractCreation uint64 = 53000
	// TxDataZeroGas 和 TxDataNonZeroGas 是 data 里每个字节再加的 gas。
	TxDataZeroGas    uint64 = 4
	TxDataNonZeroGas uint64 = 16
	// CreateDataGas 是部署代码时每个字节再加的 gas。
	CreateDataGas uint64 = 200
	// BlockGasLimit 是一个区块能装下的 gas 上限。装块时看交易的 gasLimit，收费按实际消耗。
	BlockGasLimit uint64 = 100000
)

// gasFee 是 gas 乘单价。乘积放不进 uint64 时返回溢出。
func gasFee(gas, price uint64) (uint64, error) {
	// 单价为 0 时费用是 0，不用做除法。否则先看 gas 是否大于最大值除以单价，避免乘法溢出。
	if price != 0 && gas > ^uint64(0)/price {
		return 0, ErrBalanceOverflow
	}
	return gas * price, nil
}

// intrinsicGas 是这笔交易还没执行就要扣掉的 gas。
// 转账从 IntrinsicGas 起算，创建合约从 TxGasContractCreation 起算，data 再按字节加价。
func (tx Tx) intrinsicGas() (uint64, error) {
	// 创建比转账多 32000。data 里每个零字节再加 4，非零字节再加 16。
	gas := IntrinsicGas
	if tx.Create {
		gas = TxGasContractCreation
	}
	for _, b := range tx.Data {
		part := TxDataNonZeroGas
		if b == 0 {
			part = TxDataZeroGas
		}
		// 累加这一字节的 gas 前，先看会不会超出 uint64。
		if gas > ^uint64(0)-part {
			return 0, ErrBalanceOverflow
		}
		gas += part
	}
	return gas, nil
}

// checkTxAdmission 在改状态之前检查 chain id、最低 gas 和余额。
// 预留的最高费用是 gasLimit * gasPrice，执行时按实际消耗收费。
func checkTxAdmission(tx Tx, balance Uint256) error {
	// 这些失败都发生在改状态之前，调用方可以不入块。
	if tx.ChainID != ChainID {
		return ErrWrongChainID
	}
	intrinsic, err := tx.intrinsicGas()
	if err != nil {
		return err
	}
	// gasLimit 必须盖住起步 gas，否则这笔连执行都开始不了。
	if tx.GasLimit < intrinsic {
		return ErrIntrinsicGas
	}
	cost, err := txMaxCost(tx)
	if err != nil {
		return err
	}
	// cost 是 value + gasLimit*gasPrice
	// 上面已经要求 gasLimit 不小于起步 gas，所以这里不用再加 intrinsic
	// 余额按用户授权的 gas 上限预留；实际扣费时才按用掉的 gas 算
	if cost.Cmp(balance) > 0 {
		return ErrInsufficientBalance
	}
	return nil
}

// txMaxCost 是一笔交易最多要占的余额：value 加上 gasLimit * gasPrice。
func txMaxCost(tx Tx) (Uint256, error) {
	// 先算 gasLimit * gasPrice。费用仍是 uint64，这一步溢出就不再加 value。
	maxFee, err := gasFee(tx.GasLimit, tx.GasPrice)
	if err != nil {
		return Uint256{}, err
	}
	// value 和最高费用相加超出 2^256-1 时也是溢出。
	cost, overflow := tx.Value.Add(U256(maxFee))
	if overflow {
		return Uint256{}, ErrBalanceOverflow
	}
	return cost, nil
}

// execResult 是一笔交易跑完、还没收手续费时的结果。
// GasLeft 是还没花掉的 gas。Logs 只在 Kind 为 execOK 时保留。
// Kind 是 execKind，取值 execOK、execRevert 或 execHalt。零值正好是 execOK，所以要先看 error。
// error 非空表示起步 gas 算不出来或转账失败，这笔不能入块，没有回执。
// 回退和停机都不放进 error：两种都入块，都出状态为 0 的回执(失败)。
// 回退是合约执行 REVERT，只收用掉的 gas
// 停机是虚拟机中止，剩余 gas 全部扣掉
type execResult struct {
	GasLeft uint64
	Logs    []Log
	Kind    execKind
}

// checkedSender 恢复签名，并确认 chain id、gas、余额和 nonce 都允许执行。
// 失败时状态不动。账户还不存在就当成余额 0、nonce 0，这里先不写入状态。
func checkedSender(s *State, tx Tx) (Address, error) {
	sender, err := tx.Sender()
	if err != nil {
		return Address{}, ErrBadSignature
	}
	from := s.GetAccountOrZero(sender)
	if err := checkTxAdmission(tx, from.Balance); err != nil {
		return Address{}, err
	}
	// 执行要求 nonce 正好等于账户当前值。池里允许更大的 nonce，这里不允许。
	if tx.Nonce != from.Nonce {
		return Address{}, ErrNonceMismatch
	}
	return sender, nil
}

// Apply 执行一笔交易。检查失败或余额溢出时返回错误，状态保持原样，这笔交易不能入块。
// 回退或停机时返回状态为 0 的回执：nonce 仍然加 1，这次执行改过的状态丢掉。
// 回退只收已经用掉的 gas；停机按整个 gasLimit 收。
// 成功时状态为 1。没有代码的收款方只做转账。发给自己时 value 不改变余额。
// 发送方就是 coinbase 时，费用先扣再加回。
// 过程分成三段：checkedSender 做准入，executeTx 改状态，commitExec 和 receiptFor 决定留下什么并收费。
func Apply(s *State, tx Tx) (Receipt, error) {
	sender, err := checkedSender(s, tx)
	if err != nil {
		return Receipt{}, err
	}
	// 先记 ORIGIN 和 GASPRICE，并清掉上一笔的瞬时存储。快照打在这之后，回退时这些也回到空。
	s.beginTx(sender, tx.GasPrice)
	// 先留快照。转账或跑代码改坏了，可以整份换回来。
	snap := s.Copy()
	result, err := executeTx(s, sender, tx)
	if err != nil {
		// 起步 gas 算不出来，或转账失败。这不是回退或停机。换回快照，不产生回执。
		s.restore(snap)
		return Receipt{}, err
	}
	// 成功时才删掉本笔新建后又 SELFDESTRUCT 的合约。回退和停机会在 commitExec 里换回快照。
	if result.Kind == execOK {
		s.finishSelfDestruct()
	}
	s.endTx()
	status, gasLeft, logs := commitExec(s, snap, sender, result)
	receipt, err := receiptFor(s, sender, tx, status, gasLeft, logs)
	if err != nil {
		// 费用算出来溢出，或扣费、入账失败。换回执行前的快照，这笔不入块。
		s.restore(snap)
		return Receipt{}, err
	}
	return receipt, nil
}

// commitExec 按执行结果留下该留的状态，并给出回执要用的状态、剩余 gas 和日志。
// 成功时状态保持执行后的样子，回执状态为 1，日志保留。
// 回退和停机都换回快照，再把 nonce 加 1，日志不留，回执状态为 0。
// 差别只在收费：回退的 GasLeft 是停下来时还剩的，只收已经用掉的 gas。
// 停机把 GasLeft 记为 0，剩余 gas 不退，费用按整个 gasLimit 收。
func commitExec(s, snap *State, sender Address, result execResult) (status, gasLeft uint64, logs []Log) {
	if result.Kind == execOK {
		return 1, result.GasLeft, result.Logs
	}
	// 快照里的 nonce 还是执行前的值。executeTx 里加过的那一次随快照丢掉，这里再加，回退或停机的交易仍然消耗一个 nonce。
	s.restore(snap)
	bumpNonce(s, sender)
	if result.Kind == execHalt {
		return 0, 0, nil
	}
	return 0, result.GasLeft, nil
}

// receiptFor 按用掉的 gas 收费，并拼出回执。
// 收费失败时发送方可能已经扣过费，调用方负责换回快照。
func receiptFor(s *State, sender Address, tx Tx, status, gasLeft uint64, logs []Log) (Receipt, error) {
	gasUsed := tx.GasLimit - gasLeft
	fee, err := gasFee(gasUsed, tx.GasPrice)
	if err != nil {
		return Receipt{}, err
	}
	if err := payFee(s, sender, fee); err != nil {
		return Receipt{}, err
	}
	return Receipt{Status: status, GasUsed: gasUsed, Logs: logs}, nil
}

// executeTx 先扣掉起步 gas，再把发送方 nonce 加 1，然后转账或创建。没有代码的收款方到此结束。
// gas 从 tx.GasLimit 里扣。先扣起步 gas，剩下的才给代码用；普通转账把这份剩余放进 GasLeft。
func executeTx(s *State, sender Address, tx Tx) (execResult, error) {
	// 起步 gas 在这里扣。receiptFor 用 gasLimit 减去 GasLeft，得到实际消耗。
	intrinsic, err := tx.intrinsicGas()
	if err != nil {
		return execResult{}, err
	}
	if tx.GasLimit < intrinsic {
		return execResult{}, ErrIntrinsicGas
	}
	gasLeft := tx.GasLimit - intrinsic
	// 发送方 nonce 先加 1。回退或停机时 commitExec 会先换回快照，这次加的被丢掉，然后再加一次，结果仍然只加 1。
	bumpNonce(s, sender)
	if tx.Create {
		return executeCreate(s, sender, tx, gasLeft)
	}
	// 转账失败是错误，不是回退。调用方换回快照，这笔不入块。
	if err := s.transfer(sender, tx.To, tx.Value); err != nil {
		return execResult{}, err
	}
	// 收款方没有代码就是普通转账，剩下的 gas 原样退回。
	code := s.GetCode(tx.To)
	if len(code) == 0 {
		return execResult{GasLeft: gasLeft, Kind: execOK}, nil
	}
	// depth 从 1 起。steps 是这笔交易还能跑的步数，内层 CALL 共用这一份。value 是这笔调用的金额。
	steps := maxSteps
	_, logs, left, kind := runCode(s, tx.To, sender, tx.Value, tx.Data, code, gasLeft, 1, &steps)
	return execResult{GasLeft: left, Logs: logs, Kind: kind}, nil
}

// executeCreate 用这笔交易的 tx.Nonce 算地址。新合约的 nonce 先写成 1，再跑初始代码。
// RETURN 出来的字节才存成合约代码，每字节再收 CreateDataGas。超过 maxCodeSize 整笔停机。
func executeCreate(s *State, sender Address, tx Tx, gasLeft uint64) (execResult, error) {
	// tx.Nonce 就是创建前发送方账户上的号码。bumpNonce 加的是账户，不改交易里的这个值。
	addr := CreateAddress(sender, tx.Nonce)
	// 这个地址上已经有合约时不能覆盖，整笔停机。
	if existing, ok := s.GetAccount(addr); ok && (existing.Nonce > 0 || len(s.GetCode(addr)) > 0) {
		return execResult{Kind: execHalt}, nil
	}
	// 新地址上已有余额时，再加上 value 也可能溢出。这是错误，不是停机。
	if err := s.transfer(sender, addr, tx.Value); err != nil {
		return execResult{}, err
	}
	// 规定新合约的 nonce 从 1 起，这样它自己发出的第一笔交易不能用 0。
	created, _ := s.GetAccount(addr)
	if created.Address == (Address{}) {
		created.Address = addr
	}
	created.Nonce = 1
	s.SetAccount(created)
	// 初始代码里的 SELFDESTRUCT 只有这个标记还在，交易成功结束时才会把账户删掉。
	s.markCreated(addr)
	// tx.Data 是初始代码，不是调用数据，所以 input 传 nil。value 是创建时附带的金额。
	steps := maxSteps
	ret, logs, left, kind := runCode(s, addr, sender, tx.Value, nil, tx.Data, gasLeft, 1, &steps)
	// 初始代码回退或停机时不保存代码。commitExec 会丢掉这次状态，只留 nonce 和费用。
	if kind != execOK {
		return execResult{GasLeft: left, Kind: kind}, nil
	}
	if len(ret) > maxCodeSize {
		return execResult{Kind: execHalt}, nil
	}
	// RETURN 出来的字节才是合约代码，每字节再收 200 gas。gas 不够就不部署。
	deposit := uint64(len(ret)) * CreateDataGas
	if left < deposit {
		return execResult{Kind: execHalt}, nil
	}
	s.SetCode(addr, ret)
	return execResult{GasLeft: left - deposit, Logs: logs, Kind: execOK}, nil
}

// bumpNonce 把账户 nonce 加 1。账户还不存在时先建成 nonce 0，再加，然后写进状态。
func bumpNonce(s *State, addr Address) {
	acct := s.GetAccountOrZero(addr)
	acct.Nonce++
	s.SetAccount(acct)
}

// payFee 从发送方扣除实际费用，加给 coinbase。两者是同一账户时先扣再加，费用互相抵消。
func payFee(s *State, sender Address, fee uint64) error {
	// 费用为 0 时不用动账户。
	if fee == 0 {
		return nil
	}
	from := s.GetAccountOrZero(sender)
	// 先从发送方扣。余额不够就返回错误，调用方会换回快照。
	cost := U256(fee)
	if from.Balance.Cmp(cost) < 0 {
		return ErrInsufficientBalance
	}
	next, _ := from.Balance.Sub(cost)
	from.Balance = next
	s.SetAccount(from)
	// 发送方就是 coinbase 时，这里读到的是刚扣完费的余额，再加上去就抵消了。
	coin := s.coinbase
	miner := s.GetAccountOrZero(coin)
	// coinbase 入账溢出同样返回错误。发送方刚扣掉的费用会随快照退回。
	sum, overflow := miner.Balance.Add(cost)
	if overflow {
		return ErrBalanceOverflow
	}
	miner.Balance = sum
	s.SetAccount(miner)
	return nil
}
