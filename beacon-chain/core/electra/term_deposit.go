package electra

import (
	"bytes"
	"context"

	"github.com/OffchainLabs/prysm/v6/beacon-chain/core/helpers"
	"github.com/OffchainLabs/prysm/v6/beacon-chain/core/signing"
	"github.com/OffchainLabs/prysm/v6/beacon-chain/state"
	"github.com/OffchainLabs/prysm/v6/config/params"
	"github.com/OffchainLabs/prysm/v6/consensus-types/primitives"
	"github.com/OffchainLabs/prysm/v6/crypto/bls"
	"github.com/OffchainLabs/prysm/v6/encoding/bytesutil"
	ethpb "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1"
	"github.com/OffchainLabs/prysm/v6/time/slots"
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

// GetActiveTermDepositBalance 获取验证者的有效定期存单总额
// 包括：Active 状态且未到期的存单 + Matured 状态（宽限期内）的存单
func GetActiveTermDepositBalance(
	st state.BeaconState,
	validatorIndex primitives.ValidatorIndex,
	currentEpoch primitives.Epoch,
) (uint64, error) {
	termDeposits, err := st.TermDepositsForValidator(validatorIndex)
	if err != nil {
		return 0, err
	}

	total := uint64(0)
	for _, td := range termDeposits {
		switch td.Status {
		case TermDepositStatusActive:
			// 活跃存单：未到期时计入
			// 注意：td.MaturityEpoch 是 primitives.Epoch 类型（通过 cast_type）
			if td.MaturityEpoch > currentEpoch {
				total += td.Amount
			}
		case TermDepositStatusMatured:
			// 已到期但在宽限期内：仍计入有效余额
			total += td.Amount
		}
	}
	return total, nil
}

// ProcessTermDepositMaturity 处理存单到期（每 epoch 调用）
func ProcessTermDepositMaturity(ctx context.Context, st state.BeaconState) error {
	currentEpoch := slots.ToEpoch(st.Slot())

	termDeposits, err := st.TermDeposits()
	if err != nil {
		return errors.Wrap(err, "failed to get term deposits")
	}

	for i, td := range termDeposits {
		if td.Status != TermDepositStatusActive {
			continue
		}

		// 检查是否已经到期
		if currentEpoch > td.MaturityEpoch {
			graceEndEpoch := td.MaturityEpoch + primitives.Epoch(td.GracePeriod)

			if currentEpoch > graceEndEpoch {
				// 检查是否超过最大续期次数
				maxRenewals := params.BeaconConfig().MaxTermDepositRenewals
				if maxRenewals > 0 && uint64(td.RenewalCount) >= maxRenewals {
					// 超过最大续期次数，强制转为活期
					if err := helpers.IncreaseBalance(st, primitives.ValidatorIndex(td.ValidatorIndex), td.Amount); err != nil {
						return err
					}

					td.Status = TermDepositStatusWithdrawn
					td.Amount = 0

					log.WithFields(log.Fields{
						"deposit_id":   td.DepositId,
						"validator":    td.ValidatorIndex,
						"max_renewals": maxRenewals,
					}).Info("Term deposit exceeded max renewals, converted to demand balance")
				} else {
					// 自动续期：更新时间
					newStartEpoch := graceEndEpoch + 1
					newMaturityEpoch := newStartEpoch + primitives.Epoch(td.TermDuration)

					td.StartEpoch = newStartEpoch
					td.MaturityEpoch = newMaturityEpoch
					td.RenewalCount++
					td.Status = TermDepositStatusActive

					log.WithFields(log.Fields{
						"deposit_id":    td.DepositId,
						"validator":     td.ValidatorIndex,
						"renewal_count": td.RenewalCount,
						"new_maturity":  newMaturityEpoch,
					}).Debug("Term deposit auto-renewed")
				}
			} else {
				// 进入宽限期，状态变为 Matured
				td.Status = TermDepositStatusMatured
				log.WithFields(log.Fields{
					"deposit_id": td.DepositId,
					"validator":  td.ValidatorIndex,
					"grace_end":  graceEndEpoch,
				}).Debug("Term deposit entered grace period")
			}

			if err := st.UpdateTermDepositAtIndex(uint64(i), td); err != nil {
				return errors.Wrapf(err, "failed to update term deposit %d", td.DepositId)
			}
		}
	}

	return nil
}

// ProcessPendingTermDeposits 处理待处理的定期存单
func ProcessPendingTermDeposits(ctx context.Context, st state.BeaconState) error {
	currentEpoch := slots.ToEpoch(st.Slot())
	pendingTermDeposits, err := st.PendingTermDeposits()
	if err != nil {
		return errors.Wrap(err, "failed to get pending term deposits")
	}

	if len(pendingTermDeposits) == 0 {
		return nil
	}

	processedIndices := make(map[int]bool)

	for i, ptd := range pendingTermDeposits {
		// 检查是否已最终确认
		depositEpoch := slots.ToEpoch(ptd.Slot)
		finalizedEpoch := st.FinalizedCheckpoint().Epoch
		if depositEpoch > finalizedEpoch {
			log.WithFields(log.Fields{
				"deposit_epoch":   depositEpoch,
				"finalized_epoch": finalizedEpoch,
			}).Debug("Term deposit not finalized yet, skipping")
			continue
		}

		// 验证期限有效性
		if ptd.TermDuration < params.BeaconConfig().MinTermDepositDuration ||
			ptd.TermDuration > params.BeaconConfig().MaxTermDepositDuration {
			log.WithFields(log.Fields{
				"term_duration": ptd.TermDuration,
				"min":           params.BeaconConfig().MinTermDepositDuration,
				"max":           params.BeaconConfig().MaxTermDepositDuration,
			}).Warn("Invalid term duration")
			processedIndices[i] = true // 标记为已处理（跳过）
			continue
		}

		// 查找验证者索引
		validatorIndex, exists := st.ValidatorIndexByPubkey(bytesutil.ToBytes48(ptd.PublicKey))
		if !exists {
			log.Debug("Validator not found for term deposit, skipping")
			continue
		}

		// 获取验证者信息并验证提现凭证
		validator, err := st.ValidatorAtIndexReadOnly(validatorIndex)
		if err != nil {
			return err
		}

		// 验证提现凭证匹配
		if !bytes.Equal(validator.GetWithdrawalCredentials(), ptd.WithdrawalCredentials) {
			log.WithField("validator", validatorIndex).Warn("Withdrawal credentials mismatch")
			processedIndices[i] = true
			continue
		}

		// 检查复合凭证类型
		if !validator.HasCompoundingWithdrawalCredentials() {
			log.WithFields(log.Fields{
				"validator":              validatorIndex,
				"min_activation_balance": params.BeaconConfig().MinActivationBalance,
			}).Warn("Validator does not have compounding credentials, term deposit effective balance will be capped")
		}

		// 验证 BLS 签名
		domain, err := signing.ComputeDomain(
			params.BeaconConfig().DomainDeposit,
			nil, // genesis fork version
			nil, // genesis validators root
		)
		if err != nil {
			return err
		}

		depositMessage := &ethpb.DepositMessage{
			PublicKey:             ptd.PublicKey,
			WithdrawalCredentials: ptd.WithdrawalCredentials,
			Amount:                ptd.Amount,
		}

		signingRoot, err := signing.ComputeSigningRoot(depositMessage, domain)
		if err != nil {
			return err
		}

		pubKey, err := bls.PublicKeyFromBytes(ptd.PublicKey)
		if err != nil {
			log.WithError(err).Warn("Invalid public key")
			processedIndices[i] = true
			continue
		}

		signature, err := bls.SignatureFromBytes(ptd.Signature)
		if err != nil {
			log.WithError(err).Warn("Invalid signature")
			processedIndices[i] = true
			continue
		}

		if !signature.Verify(pubKey, signingRoot[:]) {
			log.WithField("validator", validatorIndex).Warn("Invalid BLS signature for term deposit")
			processedIndices[i] = true
			continue
		}

		// 检查存单数量限制
		existingDeposits, err := st.TermDepositsForValidator(validatorIndex)
		if err != nil {
			return err
		}
		if uint64(len(existingDeposits)) >= params.BeaconConfig().MaxTermDepositsPerValidator {
			log.WithFields(log.Fields{
				"validator": validatorIndex,
				"max":       params.BeaconConfig().MaxTermDepositsPerValidator,
			}).Warn("Validator exceeds max term deposits")
			processedIndices[i] = true
			continue
		}

		// 获取下一个存单 ID
		nextId, err := st.NextTermDepositId()
		if err != nil {
			return err
		}

		// 验证宽限期范围
		gracePeriod := ptd.GracePeriod
		if gracePeriod == 0 {
			gracePeriod = params.BeaconConfig().DefaultTermDepositGracePeriod
		}

		minGracePeriod := params.BeaconConfig().MinTermDepositGracePeriod
		maxGracePeriod := params.BeaconConfig().MaxTermDepositGracePeriod
		if gracePeriod < minGracePeriod || gracePeriod > maxGracePeriod {
			log.WithFields(log.Fields{
				"grace_period": gracePeriod,
				"min":          minGracePeriod,
				"max":          maxGracePeriod,
			}).Warn("Invalid grace period, using default")
			gracePeriod = params.BeaconConfig().DefaultTermDepositGracePeriod
		}

		// 创建新存单
		termDeposit := &ethpb.TermDeposit{
			DepositId:      nextId,
			ValidatorIndex: validatorIndex,
			Amount:         ptd.Amount,
			StartEpoch:     currentEpoch,
			TermDuration:   ptd.TermDuration,
			MaturityEpoch:  currentEpoch + primitives.Epoch(ptd.TermDuration),
			GracePeriod:    gracePeriod,
			Status:         TermDepositStatusActive,
			RenewalCount:   0,
		}

		if err := st.AppendTermDeposit(termDeposit); err != nil {
			return errors.Wrap(err, "failed to append term deposit")
		}
		if err := st.SetNextTermDepositId(nextId + 1); err != nil {
			return err
		}

		log.WithFields(log.Fields{
			"deposit_id": nextId,
			"validator":  validatorIndex,
			"amount":     ptd.Amount,
			"duration":   ptd.TermDuration,
			"grace":      gracePeriod,
		}).Info("Created term deposit")

		processedIndices[i] = true
	}

	// 移除已处理的存单
	if len(processedIndices) > 0 {
		remaining := make([]*ethpb.PendingTermDeposit, 0, len(pendingTermDeposits)-len(processedIndices))
		for i, ptd := range pendingTermDeposits {
			if !processedIndices[i] {
				remaining = append(remaining, ptd)
			}
		}
		if err := st.SetPendingTermDeposits(remaining); err != nil {
			return err
		}
	}

	return nil
}

// ProcessTermWithdrawalRequests 处理存单撤出请求
func ProcessTermWithdrawalRequests(ctx context.Context, st state.BeaconState) error {
	pendingWithdrawals, err := st.PendingTermWithdrawals()
	if err != nil {
		return errors.Wrap(err, "failed to get pending term withdrawals")
	}

	if len(pendingWithdrawals) == 0 {
		return nil
	}

	for _, req := range pendingWithdrawals {
		td, _, err := st.TermDepositById(req.DepositId)
		if err != nil {
			log.WithField("deposit_id", req.DepositId).Debug("Term deposit not found, skipping withdrawal")
			continue
		}

		// 验证所有权
		if td.ValidatorIndex != req.ValidatorIndex {
			log.WithFields(log.Fields{
				"request_validator": req.ValidatorIndex,
				"deposit_owner":     td.ValidatorIndex,
				"deposit_id":        req.DepositId,
			}).Warn("Validator attempted to withdraw deposit owned by another validator")
			continue
		}

		// 计算罚没金额
		penaltyAmount := td.Amount * uint64(req.PenaltyBps) / 10000

		// 实际返还金额
		returnAmount := td.Amount - penaltyAmount

		// 罚没金额进入公共奖励池
		currentPool, err := st.TermDepositPenaltyPool()
		if err != nil {
			return err
		}
		if err := st.SetTermDepositPenaltyPool(currentPool + penaltyAmount); err != nil {
			return err
		}

		// 返还金额加入活期余额
		if err := helpers.IncreaseBalance(st, primitives.ValidatorIndex(td.ValidatorIndex), returnAmount); err != nil {
			return err
		}

		// 标记存单为已撤出
		td.Status = TermDepositStatusWithdrawn
		td.Amount = 0
		if err := st.UpdateTermDepositById(req.DepositId, td); err != nil {
			return err
		}

		log.WithFields(log.Fields{
			"deposit_id": td.DepositId,
			"validator":  td.ValidatorIndex,
			"penalty":    penaltyAmount,
			"returned":   returnAmount,
		}).Info("Processed early withdrawal for term deposit")
	}

	// 清空待处理撤出请求
	return st.SetPendingTermWithdrawals([]*ethpb.TermWithdrawalRequest{})
}

// ProcessTermDepositPenaltyDistribution 分配罚没池奖励
func ProcessTermDepositPenaltyDistribution(ctx context.Context, st state.BeaconState) error {
	currentEpoch := slots.ToEpoch(st.Slot())

	// 检查是否到了分配间隔
	lastDistributionEpoch, err := st.TermDepositPenaltyPoolLastDistributionEpoch()
	if err != nil {
		return err
	}

	distributionInterval := primitives.Epoch(params.BeaconConfig().TermDepositPenaltyPoolDistributionInterval)
	if currentEpoch < lastDistributionEpoch+distributionInterval {
		return nil // 还未到分配时间
	}

	pool, err := st.TermDepositPenaltyPool()
	if err != nil {
		return err
	}

	if pool == 0 {
		return nil // 奖励池为空
	}

	// 获取所有活跃验证者
	activeValidators, err := helpers.ActiveValidatorIndices(ctx, st, currentEpoch)
	if err != nil {
		return errors.Wrap(err, "failed to get active validator indices")
	}
	if len(activeValidators) == 0 {
		return nil
	}

	// 计算总有效余额
	totalEffectiveBalance := uint64(0)
	for _, idx := range activeValidators {
		val, err := st.ValidatorAtIndex(idx)
		if err != nil {
			continue
		}
		totalEffectiveBalance += val.EffectiveBalance
	}

	if totalEffectiveBalance == 0 {
		return nil
	}

	// 按有效余额比例分配
	distributed := uint64(0)
	for i, idx := range activeValidators {
		val, err := st.ValidatorAtIndex(idx)
		if err != nil {
			continue
		}

		var reward uint64
		if i == len(activeValidators)-1 {
			// 最后一个验证者获取剩余部分（避免精度损失）
			reward = pool - distributed
		} else {
			reward = pool * val.EffectiveBalance / totalEffectiveBalance
		}

		if reward > 0 {
			if err := helpers.IncreaseBalance(st, idx, reward); err != nil {
				return err
			}
			distributed += reward
		}
	}

	// 清空奖励池并更新分配时间
	if err := st.SetTermDepositPenaltyPool(0); err != nil {
		return err
	}

	if err := st.SetTermDepositPenaltyPoolLastDistributionEpoch(currentEpoch); err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"pool_amount":       pool,
		"active_validators": len(activeValidators),
	}).Info("Distributed penalty pool to active validators")

	return nil
}

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
