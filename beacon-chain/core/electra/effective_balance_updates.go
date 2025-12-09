package electra

import (
	"fmt"

	"github.com/OffchainLabs/prysm/v6/beacon-chain/state"
	"github.com/OffchainLabs/prysm/v6/config/params"
	"github.com/OffchainLabs/prysm/v6/consensus-types/primitives"
	ethpb "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1"
	"github.com/OffchainLabs/prysm/v6/time/slots"
	log "github.com/sirupsen/logrus"
)

// ProcessEffectiveBalanceUpdates 处理有效余额更新
//
// Spec pseudocode definition:
//
//	def process_effective_balance_updates(state: BeaconState) -> None:
//	    # Update effective balances with hysteresis
//	    for index, validator in enumerate(state.validators):
//	        balance = state.balances[index]
//	        HYSTERESIS_INCREMENT = uint64(EFFECTIVE_BALANCE_INCREMENT // HYSTERESIS_QUOTIENT)
//	        DOWNWARD_THRESHOLD = HYSTERESIS_INCREMENT * HYSTERESIS_DOWNWARD_MULTIPLIER
//	        UPWARD_THRESHOLD = HYSTERESIS_INCREMENT * HYSTERESIS_UPWARD_MULTIPLIER
//	        EFFECTIVE_BALANCE_LIMIT = (
//	            MAX_EFFECTIVE_BALANCE_EIP7251 if has_compounding_withdrawal_credential(validator)
//	            else MIN_ACTIVATION_BALANCE
//	        )
//
//	        if (
//	            balance + DOWNWARD_THRESHOLD < validator.effective_balance
//	            or validator.effective_balance + UPWARD_THRESHOLD < balance
//	        ):
//	            validator.effective_balance = min(balance - balance % EFFECTIVE_BALANCE_INCREMENT, EFFECTIVE_BALANCE_LIMIT)
func ProcessEffectiveBalanceUpdates(st state.BeaconState) error {
	effBalanceInc := params.BeaconConfig().EffectiveBalanceIncrement
	hysteresisInc := effBalanceInc / params.BeaconConfig().HysteresisQuotient
	downwardThreshold := hysteresisInc * params.BeaconConfig().HysteresisDownwardMultiplier
	upwardThreshold := hysteresisInc * params.BeaconConfig().HysteresisUpwardMultiplier

	bals := st.Balances()
	currentEpoch := slots.ToEpoch(st.Slot())

	// Update effective balances with hysteresis.
	validatorFunc := func(idx int, val state.ReadOnlyValidator) (newVal *ethpb.Validator, err error) {
		if val.IsNil() {
			return nil, fmt.Errorf("validator %d is nil in state", idx)
		}
		if idx >= len(bals) {
			return nil, fmt.Errorf("validator index exceeds validator length in state %d >= %d", idx, len(st.Balances()))
		}

		// 活期余额
		demandBalance := bals[idx]

		// 获取未过期定期存单总额（包括宽限期内的存单）
		termBalance, err := GetActiveTermDepositBalance(st, primitives.ValidatorIndex(idx), currentEpoch)
		if err != nil {
			// 如果获取失败，记录警告但继续处理（使用 0）
			log.WithError(err).WithField("validator", idx).Warn("Failed to get term deposit balance, using 0")
			termBalance = 0
		}

		// 总可用余额 = 活期 + 定期
		totalBalance := demandBalance + termBalance

		// 有效余额上限逻辑
		effectiveBalanceLimit := params.BeaconConfig().MinActivationBalance
		if val.HasCompoundingWithdrawalCredentials() {
			effectiveBalanceLimit = params.BeaconConfig().MaxEffectiveBalanceElectra
		} else if termBalance > 0 {
			// 对于有定期存单但无复合凭证的验证者，记录警告
			// 保持 MinActivationBalance 上限（更安全）
			log.WithFields(log.Fields{
				"validator":    idx,
				"term_balance": termBalance,
				"limit":        effectiveBalanceLimit,
			}).Warn("Validator has term deposits but no compounding credentials, effective balance capped at MinActivationBalance")
		}

		if totalBalance+downwardThreshold < val.EffectiveBalance() || val.EffectiveBalance()+upwardThreshold < totalBalance {
			effectiveBal := min(totalBalance-totalBalance%effBalanceInc, effectiveBalanceLimit)
			newVal = val.Copy()
			newVal.EffectiveBalance = effectiveBal
		}
		return newVal, nil
	}

	return st.ApplyToEveryValidator(validatorFunc)
}
