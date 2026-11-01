package chain

// 这个文件把一笔交易应用到状态上，并计算它的 gas。
// 转账、调用合约和创建合约都从 Apply 进入。起步 gas、data 加价和区块 gas 上限也定义在这里。
// 签名、chain id、nonce 或余额不过关时，状态保持原样，这笔不能入块。
// 执行回退或 gas 用尽时仍返回回执：nonce 加 1，费用付给 coinbase，这次改过的状态丢掉。

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

// gasProduct 是 gas 乘单价。乘积放不进 uint64 时返回溢出。
func gasProduct(gas, price uint64) (uint64, error) {
	// 单价为 0 时费用是 0，不用做除法。否则先看 gas 是否大于最大值除以单价，避免乘法溢出。
	if price != 0 && gas > ^uint64(0)/price {
		return 0, ErrBalanceOverflow
	}
	return gas * price, nil
}

// IntrinsicGasOf 是这笔交易还没执行就要扣掉的 gas。
// 转账从 21000 起算，创建合约从 53000 起算，data 里零字节加 4，非零字节加 16。
func (tx Tx) IntrinsicGasOf() (uint64, error) {
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

// checkTx 在改状态之前检查 chain id、最低 gas 和余额。
// 预留的最高费用是 gasLimit * gasPrice，执行时按实际消耗收费。
func checkTx(tx Tx, balance uint64) error {
	// 这些失败都发生在改状态之前，调用方可以不入块。
	if tx.ChainID != ChainID {
		return ErrWrongChainID
	}
	intrinsic, err := tx.IntrinsicGasOf()
	if err != nil {
		return err
	}
	// gasLimit 必须盖住起步 gas，否则这笔连执行都开始不了。
	if tx.GasLimit < intrinsic {
		return ErrIntrinsicGas
	}
	cost, err := txCost(tx)
	if err != nil {
		return err
	}
	if cost > balance {
		return ErrInsufficientBalance
	}
	return nil
}

// txCost 是一笔交易最多要占的余额：value 加上 gasLimit * gasPrice。
func txCost(tx Tx) (uint64, error) {
	// 先算 gasLimit * gasPrice。这一步溢出就不再加 value。
	maxFee, err := gasProduct(tx.GasLimit, tx.GasPrice)
	if err != nil {
		return 0, err
	}
	// value 和最高费用相加也会超出 uint64。
	if tx.Value > ^uint64(0)-maxFee {
		return 0, ErrBalanceOverflow
	}
	return tx.Value + maxFee, nil
}

// Apply 执行一笔交易。检查失败或余额溢出时返回错误，状态保持原样，这笔交易不能入块。
// 执行回退或 gas 用尽时返回状态为 0 的回执：nonce 仍然加 1，gas 照收，这次执行改过的状态丢掉。
// 成功时状态为 1。没有代码的收款方只做转账。发给自己时 value 不改变余额。
// 发送方就是 coinbase 时，费用先扣再加回。
func Apply(s *State, tx Tx) (Receipt, error) {
	// 签名恢复不出发送方时，状态不动，这笔不能入块。
	sender, err := tx.Sender()
	if err != nil {
		return Receipt{}, ErrBadSignature
	}
	// 账户还不存在就当成余额 0、nonce 0，这里先不写入状态。
	from, ok := s.Get(sender)
	if !ok {
		from = Account{Address: sender}
	}
	if err := checkTx(tx, from.Balance); err != nil {
		return Receipt{}, err
	}
	// 执行要求 nonce 正好等于账户当前值。池里允许更大的 nonce，这里不允许。
	if tx.Nonce != from.Nonce {
		return Receipt{}, ErrNonceMismatch
	}
	intrinsic, err := tx.IntrinsicGasOf()
	if err != nil {
		return Receipt{}, err
	}
	// 先留快照。转账或跑代码改坏了，可以整份换回来。
	snap := s.Copy()
	// 传给执行的 gas 已经扣掉起步 gas。剩下的才是跑代码能用的。
	gasLeft, logs, kind, err := execute(s, sender, tx, tx.GasLimit-intrinsic)
	if err != nil {
		s.restore(snap)
		return Receipt{}, err
	}
	status := uint64(1)
	if kind != execOK {
		// 回退或停机：这次改过的状态丢掉，但 nonce 仍加 1，日志不留。
		s.restore(snap)
		bumpNonce(s, sender)
		logs = nil
		status = 0
		// 停机视为 gas 用尽，费用按整个 gasLimit 收。回退则只收已经用掉的。
		if kind == execHalt {
			gasLeft = 0
		}
	}
	// 实际费用按 gasUsed 收。算出来溢出，或扣费、入账失败时，换回执行前的快照，这笔不入块。
	gasUsed := tx.GasLimit - gasLeft
	fee, err := gasProduct(gasUsed, tx.GasPrice)
	if err != nil {
		s.restore(snap)
		return Receipt{}, err
	}
	if err := payFee(s, sender, fee); err != nil {
		s.restore(snap)
		return Receipt{}, err
	}
	return Receipt{Status: status, GasUsed: gasUsed, Logs: logs}, nil
}

// execute 先把发送方 nonce 加 1，再转账或创建。没有代码的收款方到此结束。
func execute(s *State, sender Address, tx Tx, gasLeft uint64) (uint64, []Log, int, error) {
	// 发送方 nonce 先加 1。回退或停机时 Apply 会先换回快照，这次加的被丢掉，然后再加一次，结果仍然只加 1。
	bumpNonce(s, sender)
	if tx.Create {
		return executeCreate(s, sender, tx, gasLeft)
	}
	// 转账失败是错误，不是回退。调用方换回快照，这笔不入块。
	if err := s.transfer(sender, tx.To, tx.Value); err != nil {
		return 0, nil, 0, err
	}
	// 收款方没有代码就是普通转账，剩下的 gas 原样退回。
	code := s.Code(tx.To)
	if len(code) == 0 {
		return gasLeft, nil, execOK, nil
	}
	// depth 从 1 起。steps 是这笔交易还能跑的步数，内层 CALL 共用这一份。
	steps := maxSteps
	_, logs, left, kind := runCode(s, tx.To, sender, tx.Data, code, gasLeft, 1, &steps)
	return left, logs, kind, nil
}

// executeCreate 用加 1 之前的 nonce 算地址。新合约的 nonce 先写成 1，再跑初始代码。
// RETURN 出来的字节才存成合约代码，每字节再收 CreateDataGas。超过 maxCodeSize 整笔停机。
func executeCreate(s *State, sender Address, tx Tx, gasLeft uint64) (uint64, []Log, int, error) {
	// execute 已经把发送方 nonce 加过 1，地址要用创建前的那个值。
	_, nonce := s.BalanceNonce(sender)
	addr := CreateAddress(sender, nonce-1)
	// 这个地址上已经有合约时不能覆盖，整笔停机。
	if existing, ok := s.Get(addr); ok && (existing.Nonce > 0 || len(s.Code(addr)) > 0) {
		return 0, nil, execHalt, nil
	}
	// 新地址上已有余额时，再加上 value 也可能溢出。这是错误，不是停机。
	if err := s.transfer(sender, addr, tx.Value); err != nil {
		return 0, nil, 0, err
	}
	// 新合约的 nonce 从 1 起，这样它自己发出的第一笔交易不能用 0。
	created, _ := s.Get(addr)
	if created.Address == (Address{}) {
		created.Address = addr
	}
	created.Nonce = 1
	s.Put(created)
	// tx.Data 是初始代码，不是调用数据，所以 input 传 nil。depth 从 1 起，步数和调用共用。
	steps := maxSteps
	ret, logs, left, kind := runCode(s, addr, sender, nil, tx.Data, gasLeft, 1, &steps)
	// 初始代码回退或停机时不保存代码。Apply 会丢掉这次状态，只留 nonce 和费用。
	if kind != execOK {
		return left, nil, kind, nil
	}
	if len(ret) > maxCodeSize {
		return 0, nil, execHalt, nil
	}
	// RETURN 出来的字节才是合约代码，每字节再收 200 gas。gas 不够就不部署。
	deposit := uint64(len(ret)) * CreateDataGas
	if left < deposit {
		return 0, nil, execHalt, nil
	}
	s.SetCode(addr, ret)
	return left - deposit, logs, execOK, nil
}

// bumpNonce 把账户 nonce 加 1。账户还不存在时先建成 nonce 0，再加，然后写进状态。
func bumpNonce(s *State, addr Address) {
	acct, ok := s.Get(addr)
	if !ok {
		acct = Account{Address: addr}
	}
	acct.Nonce++
	s.Put(acct)
}

// payFee 从发送方扣除实际费用，加给 coinbase。两者是同一账户时先扣再加，费用互相抵消。
func payFee(s *State, sender Address, fee uint64) error {
	// 费用为 0 时不用动账户。
	if fee == 0 {
		return nil
	}
	from, ok := s.Get(sender)
	if !ok {
		from = Account{Address: sender}
	}
	// 先从发送方扣。余额不够就返回错误，调用方会换回快照。
	if from.Balance < fee {
		return ErrInsufficientBalance
	}
	from.Balance -= fee
	s.Put(from)
	// 发送方就是 coinbase 时，这里读到的是刚扣完费的余额，再加上去就抵消了。
	coin := s.coinbase
	miner, ok := s.Get(coin)
	if !ok {
		miner = Account{Address: coin}
	}
	// coinbase 入账溢出同样返回错误。发送方刚扣掉的费用会随快照退回。
	if miner.Balance > ^uint64(0)-fee {
		return ErrBalanceOverflow
	}
	miner.Balance += fee
	s.Put(miner)
	return nil
}
