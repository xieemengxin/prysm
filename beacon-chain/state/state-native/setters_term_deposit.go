package state_native

import (
	"errors"

	"github.com/OffchainLabs/prysm/v6/beacon-chain/state/state-native/types"
	"github.com/OffchainLabs/prysm/v6/beacon-chain/state/stateutil"
	"github.com/OffchainLabs/prysm/v6/consensus-types/primitives"
	ethpb "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1"
	"github.com/OffchainLabs/prysm/v6/runtime/version"
)

// AppendTermDeposit 添加一个新的定期存单
func (b *BeaconState) AppendTermDeposit(td *ethpb.TermDeposit) error {
	if b.version < version.Electra {
		return errNotSupported("AppendTermDeposit", b.version)
	}
	if td == nil {
		return errors.New("cannot append nil term deposit")
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	termDeposits := b.termDeposits
	if b.sharedFieldReferences[types.TermDeposits].Refs() > 1 {
		termDeposits = make([]*ethpb.TermDeposit, 0, len(b.termDeposits)+1)
		termDeposits = append(termDeposits, b.termDeposits...)
		b.sharedFieldReferences[types.TermDeposits].MinusRef()
		b.sharedFieldReferences[types.TermDeposits] = stateutil.NewRef(1)
	}

	b.termDeposits = append(termDeposits, td)
	b.markFieldAsDirty(types.TermDeposits)

	return nil
}

// UpdateTermDepositAtIndex 更新指定索引的定期存单
func (b *BeaconState) UpdateTermDepositAtIndex(idx uint64, td *ethpb.TermDeposit) error {
	if b.version < version.Electra {
		return errNotSupported("UpdateTermDepositAtIndex", b.version)
	}
	if td == nil {
		return errors.New("cannot update with nil term deposit")
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	if idx >= uint64(len(b.termDeposits)) {
		return errors.New("index out of range")
	}

	if b.sharedFieldReferences[types.TermDeposits].Refs() > 1 {
		termDeposits := make([]*ethpb.TermDeposit, len(b.termDeposits))
		copy(termDeposits, b.termDeposits)
		b.sharedFieldReferences[types.TermDeposits].MinusRef()
		b.sharedFieldReferences[types.TermDeposits] = stateutil.NewRef(1)
		b.termDeposits = termDeposits
	}

	b.termDeposits[idx] = td
	b.markFieldAsDirty(types.TermDeposits)
	b.addDirtyIndices(types.TermDeposits, []uint64{idx})

	return nil
}

// UpdateTermDepositById 根据存单 ID 更新存单
func (b *BeaconState) UpdateTermDepositById(depositId uint64, td *ethpb.TermDeposit) error {
	if b.version < version.Electra {
		return errNotSupported("UpdateTermDepositById", b.version)
	}
	if td == nil {
		return errors.New("cannot update with nil term deposit")
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	for i, existing := range b.termDeposits {
		if existing.DepositId == depositId {
			if b.sharedFieldReferences[types.TermDeposits].Refs() > 1 {
				termDeposits := make([]*ethpb.TermDeposit, len(b.termDeposits))
				copy(termDeposits, b.termDeposits)
				b.sharedFieldReferences[types.TermDeposits].MinusRef()
				b.sharedFieldReferences[types.TermDeposits] = stateutil.NewRef(1)
				b.termDeposits = termDeposits
			}

			b.termDeposits[i] = td
			b.markFieldAsDirty(types.TermDeposits)
			b.addDirtyIndices(types.TermDeposits, []uint64{uint64(i)})
			return nil
		}
	}

	return errors.New("term deposit not found")
}

// SetTermDeposits 设置所有定期存单
func (b *BeaconState) SetTermDeposits(tds []*ethpb.TermDeposit) error {
	if b.version < version.Electra {
		return errNotSupported("SetTermDeposits", b.version)
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	b.sharedFieldReferences[types.TermDeposits].MinusRef()
	b.sharedFieldReferences[types.TermDeposits] = stateutil.NewRef(1)

	b.termDeposits = tds
	b.markFieldAsDirty(types.TermDeposits)
	return nil
}

// SetPendingTermDeposits 设置待处理的定期存单队列
func (b *BeaconState) SetPendingTermDeposits(ptds []*ethpb.PendingTermDeposit) error {
	if b.version < version.Electra {
		return errNotSupported("SetPendingTermDeposits", b.version)
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	b.sharedFieldReferences[types.PendingTermDeposits].MinusRef()
	b.sharedFieldReferences[types.PendingTermDeposits] = stateutil.NewRef(1)

	b.pendingTermDeposits = ptds
	b.markFieldAsDirty(types.PendingTermDeposits)
	return nil
}

// AppendPendingTermDeposit 添加一个待处理的定期存单
func (b *BeaconState) AppendPendingTermDeposit(ptd *ethpb.PendingTermDeposit) error {
	if b.version < version.Electra {
		return errNotSupported("AppendPendingTermDeposit", b.version)
	}
	if ptd == nil {
		return errors.New("cannot append nil pending term deposit")
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	pendingTermDeposits := b.pendingTermDeposits
	if b.sharedFieldReferences[types.PendingTermDeposits].Refs() > 1 {
		pendingTermDeposits = make([]*ethpb.PendingTermDeposit, 0, len(b.pendingTermDeposits)+1)
		pendingTermDeposits = append(pendingTermDeposits, b.pendingTermDeposits...)
		b.sharedFieldReferences[types.PendingTermDeposits].MinusRef()
		b.sharedFieldReferences[types.PendingTermDeposits] = stateutil.NewRef(1)
	}

	b.pendingTermDeposits = append(pendingTermDeposits, ptd)
	b.markFieldAsDirty(types.PendingTermDeposits)

	return nil
}

// SetPendingTermWithdrawals 设置待处理的撤出请求队列
func (b *BeaconState) SetPendingTermWithdrawals(reqs []*ethpb.TermWithdrawalRequest) error {
	if b.version < version.Electra {
		return errNotSupported("SetPendingTermWithdrawals", b.version)
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	b.sharedFieldReferences[types.PendingTermWithdrawals].MinusRef()
	b.sharedFieldReferences[types.PendingTermWithdrawals] = stateutil.NewRef(1)

	b.pendingTermWithdrawals = reqs
	b.markFieldAsDirty(types.PendingTermWithdrawals)
	return nil
}

// SetNextTermDepositId 设置下一个存单 ID
func (b *BeaconState) SetNextTermDepositId(id uint64) error {
	if b.version < version.Electra {
		return errNotSupported("SetNextTermDepositId", b.version)
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	b.nextTermDepositId = id
	b.markFieldAsDirty(types.NextTermDepositId)
	return nil
}

// SetTermDepositPenaltyPool 设置罚没池余额
func (b *BeaconState) SetTermDepositPenaltyPool(amount uint64) error {
	if b.version < version.Electra {
		return errNotSupported("SetTermDepositPenaltyPool", b.version)
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	b.termDepositPenaltyPool = amount
	b.markFieldAsDirty(types.TermDepositPenaltyPool)
	return nil
}

// SetTermDepositPenaltyPoolLastDistributionEpoch 设置罚没池上次分配的 epoch
func (b *BeaconState) SetTermDepositPenaltyPoolLastDistributionEpoch(epoch primitives.Epoch) error {
	if b.version < version.Electra {
		return errNotSupported("SetTermDepositPenaltyPoolLastDistributionEpoch", b.version)
	}
	b.lock.Lock()
	defer b.lock.Unlock()

	b.termDepositPenaltyPoolLastDistEpoch = epoch
	b.markFieldAsDirty(types.TermDepositPenaltyPoolLastDistEpoch)
	return nil
}
