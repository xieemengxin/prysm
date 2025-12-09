package state_native

import (
	"github.com/OffchainLabs/prysm/v6/consensus-types/primitives"
	ethpb "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1"
	"github.com/OffchainLabs/prysm/v6/runtime/version"
	"github.com/pkg/errors"
)

// TermDeposits 返回所有定期存单的深拷贝
func (b *BeaconState) TermDeposits() ([]*ethpb.TermDeposit, error) {
	if b.version < version.Electra {
		return nil, errNotSupported("TermDeposits", b.version)
	}
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.termDepositsVal(), nil
}

func (b *BeaconState) termDepositsVal() []*ethpb.TermDeposit {
	if b.termDeposits == nil {
		return nil
	}
	// 创建深拷贝
	res := make([]*ethpb.TermDeposit, len(b.termDeposits))
	for i, td := range b.termDeposits {
		res[i] = &ethpb.TermDeposit{
			DepositId:      td.DepositId,
			ValidatorIndex: td.ValidatorIndex,
			Amount:         td.Amount,
			StartEpoch:     td.StartEpoch,
			TermDuration:   td.TermDuration,
			MaturityEpoch:  td.MaturityEpoch,
			GracePeriod:    td.GracePeriod,
			Status:         td.Status,
			RenewalCount:   td.RenewalCount,
		}
	}
	return res
}

// TermDepositsForValidator 返回指定验证者的所有定期存单
func (b *BeaconState) TermDepositsForValidator(idx primitives.ValidatorIndex) ([]*ethpb.TermDeposit, error) {
	if b.version < version.Electra {
		return nil, errNotSupported("TermDepositsForValidator", b.version)
	}
	b.lock.RLock()
	defer b.lock.RUnlock()

	var result []*ethpb.TermDeposit
	for _, td := range b.termDeposits {
		if primitives.ValidatorIndex(td.ValidatorIndex) == idx {
			// 创建拷贝
			tdCopy := &ethpb.TermDeposit{
				DepositId:      td.DepositId,
				ValidatorIndex: td.ValidatorIndex,
				Amount:         td.Amount,
				StartEpoch:     td.StartEpoch,
				TermDuration:   td.TermDuration,
				MaturityEpoch:  td.MaturityEpoch,
				GracePeriod:    td.GracePeriod,
				Status:         td.Status,
				RenewalCount:   td.RenewalCount,
			}
			result = append(result, tdCopy)
		}
	}
	return result, nil
}

// TermDepositById 根据 ID 查询存单，返回存单和其在数组中的索引
func (b *BeaconState) TermDepositById(depositId uint64) (*ethpb.TermDeposit, uint64, error) {
	if b.version < version.Electra {
		return nil, 0, errNotSupported("TermDepositById", b.version)
	}
	b.lock.RLock()
	defer b.lock.RUnlock()

	for i, td := range b.termDeposits {
		if td.DepositId == depositId {
			tdCopy := &ethpb.TermDeposit{
				DepositId:      td.DepositId,
				ValidatorIndex: td.ValidatorIndex,
				Amount:         td.Amount,
				StartEpoch:     td.StartEpoch,
				TermDuration:   td.TermDuration,
				MaturityEpoch:  td.MaturityEpoch,
				GracePeriod:    td.GracePeriod,
				Status:         td.Status,
				RenewalCount:   td.RenewalCount,
			}
			return tdCopy, uint64(i), nil
		}
	}
	return nil, 0, errors.New("term deposit not found")
}

// PendingTermDeposits 返回待处理的定期存单队列的深拷贝
func (b *BeaconState) PendingTermDeposits() ([]*ethpb.PendingTermDeposit, error) {
	if b.version < version.Electra {
		return nil, errNotSupported("PendingTermDeposits", b.version)
	}
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.pendingTermDepositsVal(), nil
}

func (b *BeaconState) pendingTermDepositsVal() []*ethpb.PendingTermDeposit {
	if b.pendingTermDeposits == nil {
		return nil
	}
	res := make([]*ethpb.PendingTermDeposit, len(b.pendingTermDeposits))
	for i, ptd := range b.pendingTermDeposits {
		res[i] = &ethpb.PendingTermDeposit{
			PublicKey:             append([]byte{}, ptd.PublicKey...),
			WithdrawalCredentials: append([]byte{}, ptd.WithdrawalCredentials...),
			Amount:                ptd.Amount,
			Signature:             append([]byte{}, ptd.Signature...),
			Slot:                  ptd.Slot,
			TermDuration:          ptd.TermDuration,
			GracePeriod:           ptd.GracePeriod,
		}
	}
	return res
}

// PendingTermWithdrawals 返回待处理的撤出请求队列的深拷贝
func (b *BeaconState) PendingTermWithdrawals() ([]*ethpb.TermWithdrawalRequest, error) {
	if b.version < version.Electra {
		return nil, errNotSupported("PendingTermWithdrawals", b.version)
	}
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.pendingTermWithdrawalsVal(), nil
}

func (b *BeaconState) pendingTermWithdrawalsVal() []*ethpb.TermWithdrawalRequest {
	if b.pendingTermWithdrawals == nil {
		return nil
	}
	res := make([]*ethpb.TermWithdrawalRequest, len(b.pendingTermWithdrawals))
	for i, req := range b.pendingTermWithdrawals {
		res[i] = &ethpb.TermWithdrawalRequest{
			DepositId:      req.DepositId,
			ValidatorIndex: req.ValidatorIndex,
			PenaltyBps:     req.PenaltyBps,
			Slot:           req.Slot,
		}
	}
	return res
}

// NextTermDepositId 返回下一个存单 ID
func (b *BeaconState) NextTermDepositId() (uint64, error) {
	if b.version < version.Electra {
		return 0, errNotSupported("NextTermDepositId", b.version)
	}
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.nextTermDepositId, nil
}

// TermDepositPenaltyPool 返回罚没池余额
func (b *BeaconState) TermDepositPenaltyPool() (uint64, error) {
	if b.version < version.Electra {
		return 0, errNotSupported("TermDepositPenaltyPool", b.version)
	}
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.termDepositPenaltyPool, nil
}

// TermDepositPenaltyPoolLastDistributionEpoch 返回罚没池上次分配的 epoch
func (b *BeaconState) TermDepositPenaltyPoolLastDistributionEpoch() (primitives.Epoch, error) {
	if b.version < version.Electra {
		return 0, errNotSupported("TermDepositPenaltyPoolLastDistributionEpoch", b.version)
	}
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.termDepositPenaltyPoolLastDistEpoch, nil
}
