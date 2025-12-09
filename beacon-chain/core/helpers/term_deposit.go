package helpers

import (
	"github.com/OffchainLabs/prysm/v6/beacon-chain/state"
	"github.com/OffchainLabs/prysm/v6/consensus-types/primitives"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

// TermDeposit 状态常量
const (
	TermDepositStatusActive            = uint64(0) // 活跃（未到期）
	TermDepositStatusMatured           = uint64(1) // 已到期（宽限期内）
	TermDepositStatusWithdrawn         = uint64(2) // 已撤出
	TermDepositStatusPendingWithdrawal = uint64(3) // 撤出中
)

// SlashTermDeposits 处理 Slashing 时的定期存单扣除
// 返回实际从定期存单扣除的金额
func SlashTermDeposits(
	st state.BeaconState,
	validatorIndex primitives.ValidatorIndex,
	remainingPenalty uint64,
) (uint64, error) {
	if remainingPenalty == 0 {
		return 0, nil
	}

	termDeposits, err := st.TermDepositsForValidator(validatorIndex)
	if err != nil {
		return 0, err
	}

	// 预先计算所有变化
	type depositUpdate struct {
		depositId      uint64
		deduction      uint64
		shouldWithdraw bool
		newAmount      uint64
	}

	updates := make([]depositUpdate, 0)
	totalDeducted := uint64(0)
	pendingPenalty := remainingPenalty

	for _, td := range termDeposits {
		if pendingPenalty == 0 {
			break
		}

		if td.Status == TermDepositStatusWithdrawn {
			continue
		}

		// 从存单中扣除
		deduction := td.Amount
		if deduction > pendingPenalty {
			deduction = pendingPenalty
		}
		pendingPenalty -= deduction
		totalDeducted += deduction

		updates = append(updates, depositUpdate{
			depositId:      td.DepositId,
			deduction:      deduction,
			shouldWithdraw: (td.Amount == deduction),
			newAmount:      td.Amount - deduction,
		})
	}

	if totalDeducted == 0 {
		return 0, nil // 没有可扣除的定期存单
	}

	// 原子性应用所有变化
	for _, update := range updates {
		td, _, err := st.TermDepositById(update.depositId)
		if err != nil {
			return 0, errors.Wrapf(err, "failed to get term deposit %d during slashing", update.depositId)
		}

		td.Amount = update.newAmount
		if update.shouldWithdraw {
			td.Status = TermDepositStatusWithdrawn
		}

		if err := st.UpdateTermDepositById(update.depositId, td); err != nil {
			return 0, errors.Wrapf(err, "failed to update term deposit %d during slashing", update.depositId)
		}
	}

	// 更新罚没池
	currentPenaltyPool, err := st.TermDepositPenaltyPool()
	if err != nil {
		return 0, err
	}

	if err := st.SetTermDepositPenaltyPool(currentPenaltyPool + totalDeducted); err != nil {
		return 0, err
	}

	log.WithFields(log.Fields{
		"validator":      validatorIndex,
		"total_slashed":  totalDeducted,
		"deposits_count": len(updates),
	}).Info("Slashed term deposits")

	return totalDeducted, nil
}
