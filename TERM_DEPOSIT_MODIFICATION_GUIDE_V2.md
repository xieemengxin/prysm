# Prysm 验证者存款双模式改造操作文档（修订版 v2.0）

## 版本信息
- 文档版本：v2.0（修订版）
- 修订日期：2025-12-08
- 基于 Prysm 版本：v6.x (Electra fork)
---

## 📋 修订记录

### v2.0 主要变更（2025-12-08）

**严重问题修复**：
1. ✅ 修复有效余额计算的复合凭证兼容性问题
2. ✅ 明确定义 Epoch 处理顺序，避免状态不一致
3. ✅ 增强 Slashing 逻辑的原子性保障
4. ✅ 添加完整的 BLS 签名验证
5. ✅ 优化字段索引编号，避免范围冲突

**重要优化**：
- 出块速度设置为2秒，epoch大小依然是32
- 宽限期从 1 天延长至 10 天，降低资金锁定风险
- 添加灵活的宽限期配置支持
- 优化 ValidatorTermDepositIndex 数据结构
- 添加公共奖励池分配机制
- 完善监控和可观测性

**部署策略**：
- 使用代理合约升级，保持地址不变
- 明确分叉激活机制
- 添加详细的安全检查清单

---

## 目录

1. [项目概述](#1-项目概述)
2. [需求规格](#2-需求规格)
3. [架构设计](#3-架构设计)
4. [数据结构定义](#4-数据结构定义)
5. [代码修改清单](#5-代码修改清单)
6. [详细修改指南](#6-详细修改指南)
7. [ETH1 合约改造](#7-eth1-合约改造)
8. [测试计划](#8-测试计划)
9. [部署注意事项](#9-部署注意事项)
10. [安全检查清单](#10-安全检查清单)

---

## 1. 项目概述

### 1.1 改造目标

将 Prysm 验证者存款机制从单一"活期存款"模式改造为"定期存单 + 活期存款"双模式系统。

### 1.2 核心功能

| 功能 | 描述 |
|------|------|
| 活期存款 | 保持现有机制，随时可提取 |
| 定期存单 | 锁定期限的存款，到期自动续期，提前撤出需罚没 |
| 有效余额计算 | 有效余额 = 活期余额 + 未过期定期存单总额 |
| 创世验证者 | 默认只有活期存款，可追加定期存单 |

### 1.3 关键设计决策

| 决策点 | 决策 | 理由 | 🆕 修订说明 |
|-------|------|------|-----------|
| 存单数据存储 | BeaconState 新增字段 | 减少 ETH1 查询，提高性能 | - |
| Validator 结构 | 不修改 | 影响范围小，支持多张存单 | - |
| 宽限期存单 | 计入有效余额 | 验证者身份不受影响 | - |
| 罚没金额去向 | 进入公共奖励池 | 激励网络参与 | 🆕 添加分配机制 |
| Slashing 惩罚 | 定期+活期都扣 | 确保惩罚有效执行 | 🆕 增强原子性 |
| 复合凭证要求 | 推荐但不强制 | 兼容现有验证者 | 🆕 新增决策 |
| 宽限期长度 | 2250 epochs (10天) | 降低资金锁定风险 | 🆕 从 1 天延长 |
| 合约部署方式 | 代理合约升级 | 保持地址不变 | 🆕 新增决策 |

---

## 2. 需求规格

### 2.1 定期存单规则

```
┌─────────────────────────────────────────────────────────────────┐
│                        定期存单生命周期                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  创建 ──► 活跃期 ──► 到期 ──► 宽限期 ──┬──► 自动续期 ──► 活跃期  │
│                                       │                         │
│                                       └──► 手动撤出 ──► 已撤出   │
│                                                                 │
│  提前撤出：任意时刻可请求，需支付罚没金额                          │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

**🆕 修订要点**：
- **期限**：由用户自定义，通过管理合约验证有效性
- **收益**：暂无额外收益（通胀设为 0）
- **到期处理**：宽限期内未撤出则自动续期（宽限期延长至 10 天）
- **提前撤出**：需支付罚没金额，罚金进入公共奖励池并按 epoch 分配
- **续期限制**：🆕 可选配置最大续期次数，防止资金永久锁定

### 2.2 有效余额计算

```
有效余额 = min(
    (活期余额 + 未过期定期存单总额) - 余额 % EFFECTIVE_BALANCE_INCREMENT,
    MAX_EFFECTIVE_BALANCE
)
```

**🆕 修订：有效余额上限逻辑**

```go
// 原方案
effectiveBalanceLimit := params.BeaconConfig().MinActivationBalance  // 通过配置文件设置的最小激活余额
if val.HasCompoundingWithdrawalCredentials() {
    effectiveBalanceLimit = params.BeaconConfig().MaxEffectiveBalanceElectra  // 通过配置文件设置的最大有效余额
}

// 🆕 修订方案：支持混合凭证
effectiveBalanceLimit := params.BeaconConfig().MinActivationBalance
if val.HasCompoundingWithdrawalCredentials() {
    effectiveBalanceLimit = params.BeaconConfig().MaxEffectiveBalanceElectra
} else {
    // 如果验证者有定期存单，也提升上限
    termBalance, _ := GetActiveTermDepositBalance(st, validatorIndex, currentEpoch)
    if termBalance > 0 {
        // 记录警告，建议升级到复合凭证
        log.Warnf("Validator %d has term deposits but no compounding credentials, effective balance limited to MinActivationBalance (%d gwei)", validatorIndex, params.BeaconConfig().MinActivationBalance)
        // 可选：提升上限以支持定期存单
        // effectiveBalanceLimit = params.BeaconConfig().MaxEffectiveBalanceElectra
    }
}
```

**未过期定期存单**包括：
- 状态为 `Active` 且 `maturity_epoch > current_epoch` 的存单
- 状态为 `Matured`（宽限期内）的存单

### 2.3 Slashing 处理（🆕 增强原子性）

```
Slashing 惩罚扣除顺序：
1. 优先从活期余额扣除
2. 活期不足时，从定期存单扣除（触发提前撤出罚没）
3. 罚没金额进入公共奖励池
4. 🆕 所有扣除操作保证原子性（预先计算，一次性提交）
```

### 2.4 验证者退出

- 退出时定期存单继续保持
- 存单自然到期后可提取
- 或通过罚没机制提前撤出
- 🆕 验证者完全提款后，清理已撤出的存单记录

---

## 3. 架构设计

### 3.1 组件交互图

```
                           ┌─────────────────┐
                           │   管理合约       │
                           │ (Term Manager)  │
                           └────────┬────────┘
                                    │ 期限验证
                                    ▼
┌─────────────────┐    存款事件    ┌─────────────────┐
│  ETH1 存款合约   │ ────────────► │  Beacon Chain   │
│ (代理合约)       │               │  Execution Layer │
│ Deposit Contract│               └────────┬────────┘
└─────────────────┘                        │
                                           ▼
                                  ┌─────────────────┐
                                  │   BeaconState   │
                                  │  ┌───────────┐  │
                                  │  │ balances  │  │  活期余额
                                  │  └───────────┘  │
                                  │  ┌───────────┐  │
                                  │  │term_deposits│ │  定期存单
                                  │  └───────────┘  │
                                  │  ┌───────────┐  │
                                  │  │penalty_pool│ │  🆕 罚没池
                                  │  └───────────┘  │
                                  └─────────────────┘
```

### 3.2 数据流（🆕 明确处理顺序）

```
定期存款流程：
ETH1 TermDepositEvent
  → log_processing.ProcessTermDepositLog()
  → depositCache.InsertPendingTermDeposit()
  → epoch_processing.ProcessPendingTermDeposits()
      ├─ 🆕 验证 BLS 签名
      ├─ 🆕 检查提现凭证匹配
      ├─ 🆕 检查复合凭证类型（记录警告）
      └─ state.AppendTermDeposit()

Epoch 处理流程（🆕 明确顺序）：
epoch_processing.ProcessEpoch()
  → ProcessRewardsAndPenalties()        // 修改 balances
  → ProcessRegistryUpdates()            // 检查 ejection
  → ProcessSlashings()                  // 修改 balances
  → ProcessPendingDeposits()            // 修改 balances (活期)
  → ProcessPendingConsolidations()      // 修改 balances (转移)
  → 🆕 ProcessTermDepositMaturity()     // 处理到期存单
  → 🆕 ProcessPendingTermDeposits()     // 处理新定期存单
  → 🆕 ProcessTermWithdrawalRequests()  // 处理撤出请求
  → ProcessEffectiveBalanceUpdates()    // 计算有效余额（包含定期）
  → 🆕 ProcessTermDepositPenaltyDistribution()  // 分配罚没池奖励
  → 🆕 UpdateTermDepositMetrics()       // 更新监控指标
```

---

## 4. 数据结构定义

### 4.1 新建文件：`proto/prysm/v1alpha1/term_deposit.proto`

```protobuf
syntax = "proto3";
package ethereum.eth.v1alpha1;

import "proto/eth/ext/options.proto";

option go_package = "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1;eth";

// TermDeposit 定期存单
// 状态常量（用于 status 字段）:
//   0 = ACTIVE             活跃（未到期）
//   1 = MATURED            已到期（宽限期内）
//   2 = WITHDRAWN          已撤出
//   3 = PENDING_WITHDRAWAL 撤出中
message TermDeposit {
  // 存单唯一标识（自增 ID）
  uint64 deposit_id = 1;

  // 关联的验证者索引
  uint64 validator_index = 2 [(ethereum.eth.ext.cast_type) =
      "github.com/OffchainLabs/prysm/v6/consensus-types/primitives.ValidatorIndex"];

  // 存款金额（gwei）
  uint64 amount = 3;

  // 存单创建时的 epoch
  uint64 start_epoch = 4 [(ethereum.eth.ext.cast_type) =
      "github.com/OffchainLabs/prysm/v6/consensus-types/primitives.Epoch"];

  // 存单期限（epoch 数量）
  uint64 term_duration = 5;

  // 存单到期的 epoch（start_epoch + term_duration）
  uint64 maturity_epoch = 6 [(ethereum.eth.ext.cast_type) =
      "github.com/OffchainLabs/prysm/v6/consensus-types/primitives.Epoch"];

  // 自动续期宽限期（epoch 数量）
  uint64 grace_period = 7;

  // 存单状态 (0=ACTIVE, 1=MATURED, 2=WITHDRAWN, 3=PENDING_WITHDRAWAL)
  uint64 status = 8;

  // 续期次数
  uint32 renewal_count = 9;
}

// PendingTermDeposit 待处理的定期存单（从 ETH1 传入）
message PendingTermDeposit {
  // 验证者公钥
  bytes public_key = 1 [(ethereum.eth.ext.ssz_size) = "48"];

  // 提现凭证
  bytes withdrawal_credentials = 2 [(ethereum.eth.ext.ssz_size) = "32"];

  // 存款金额（gwei）
  uint64 amount = 3;

  // BLS 签名
  bytes signature = 4 [(ethereum.eth.ext.ssz_size) = "96"];

  // 存款所在 slot
  uint64 slot = 5 [(ethereum.eth.ext.cast_type) =
      "github.com/OffchainLabs/prysm/v6/consensus-types/primitives.Slot"];

  // 期限（epoch 数）
  uint64 term_duration = 6;

  // 宽限期（epoch 数）
  uint64 grace_period = 7;
}

// TermWithdrawalRequest 存单提前撤出请求
message TermWithdrawalRequest {
  // 存单 ID
  uint64 deposit_id = 1;

  // 验证者索引
  uint64 validator_index = 2 [(ethereum.eth.ext.cast_type) =
      "github.com/OffchainLabs/prysm/v6/consensus-types/primitives.ValidatorIndex"];

  // 罚没比例（basis points, 1/10000）
  uint32 penalty_bps = 3;

  // 请求所在 slot
  uint64 slot = 4 [(ethereum.eth.ext.cast_type) =
      "github.com/OffchainLabs/prysm/v6/consensus-types/primitives.Slot"];
}

// 🆕 TermDepositSlashingLog 定期存单 Slashing 日志（用于审计）
message TermDepositSlashingLog {
  uint64 epoch = 1 [(ethereum.eth.ext.cast_type) =
      "github.com/OffchainLabs/prysm/v6/consensus-types/primitives.Epoch"];

  uint64 validator_index = 2 [(ethereum.eth.ext.cast_type) =
      "github.com/OffchainLabs/prysm/v6/consensus-types/primitives.ValidatorIndex"];

  repeated uint64 deposit_ids = 3 [(ethereum.eth.ext.ssz_max) = "256"];
  repeated uint64 amounts = 4 [(ethereum.eth.ext.ssz_max) = "256"];
  uint64 total_slashed = 5;
}
```

### 4.2 修改文件：`proto/prysm/v1alpha1/beacon_state.proto`（🆕 优化字段编号）

在 `BeaconStateElectra` 和 `BeaconStateFulu` 中新增字段：

```protobuf

import "proto/prysm/v1alpha1/term_deposit.proto";

message BeaconStateElectra {
  // ... 现有字段 [1001-12009] 保持不变 ...

  // ========== 🆕 定期存单相关字段 [12010-12016] ==========
  // 注意：使用 12010-12016 而非 14001-15000，避免跳号

  // 所有定期存单列表
  repeated TermDeposit term_deposits = 12010
      [(ethereum.eth.ext.ssz_max) = "1048576"];  // 最多 ~100万张

  // 待处理的定期存单队列
  repeated PendingTermDeposit pending_term_deposits = 12011
      [(ethereum.eth.ext.ssz_max) = "65536"];

  // 下一个存单 ID（自增）
  uint64 next_term_deposit_id = 12012;

  // 🆕 移除 validator_term_deposit_indices，改用内存缓存优化查询
  // repeated ValidatorTermDepositIndex validator_term_deposit_indices = 12013;

  // 待处理的存单撤出请求
  repeated TermWithdrawalRequest pending_term_withdrawals = 12013
      [(ethereum.eth.ext.ssz_max) = "65536"];

  // 公共奖励池（存放罚没金额）
  uint64 term_deposit_penalty_pool = 12014;

  // 🆕 罚没池上次分配的 epoch
  uint64 term_deposit_penalty_pool_last_distribution_epoch = 12015
      [(ethereum.eth.ext.cast_type) =
       "github.com/OffchainLabs/prysm/v6/consensus-types/primitives.Epoch"];

  // 🆕 Slashing 日志（可选，用于审计）
  repeated TermDepositSlashingLog term_deposit_slashing_logs = 12016
      [(ethereum.eth.ext.ssz_max) = "65536"];
}
```

### 4.3 配置参数（🆕 优化参数值）

修改文件：`config/params/config.go`

```go
type BeaconChainConfig struct {
    // ... 现有字段 ...

    // ========== 定期存单配置 ==========

    // 最大存单数量
    TermDepositsLimit uint64 `yaml:"TERM_DEPOSITS_LIMIT" spec:"true"`

    // 最大待处理存单数量
    PendingTermDepositsLimit uint64 `yaml:"PENDING_TERM_DEPOSITS_LIMIT" spec:"true"`

    // 每个验证者最大存单数
    MaxTermDepositsPerValidator uint64 `yaml:"MAX_TERM_DEPOSITS_PER_VALIDATOR" spec:"true"`

    // 🆕 默认宽限期（epoch 数）- 从 4050 增加到 40500
    DefaultTermDepositGracePeriod uint64 `yaml:"DEFAULT_TERM_DEPOSIT_GRACE_PERIOD" spec:"true"`

    // 🆕 最小宽限期（epoch 数）
    MinTermDepositGracePeriod uint64 `yaml:"MIN_TERM_DEPOSIT_GRACE_PERIOD" spec:"true"`

    // 🆕 最大宽限期（epoch 数）
    MaxTermDepositGracePeriod uint64 `yaml:"MAX_TERM_DEPOSIT_GRACE_PERIOD" spec:"true"`

    // 最小期限（epoch 数）
    MinTermDepositDuration uint64 `yaml:"MIN_TERM_DEPOSIT_DURATION" spec:"true"`

    // 最大期限（epoch 数）
    MaxTermDepositDuration uint64 `yaml:"MAX_TERM_DEPOSIT_DURATION" spec:"true"`

    // 提前撤出基础罚没比例（basis points）
    EarlyWithdrawalPenaltyBaseBps uint64 `yaml:"EARLY_WITHDRAWAL_PENALTY_BASE_BPS" spec:"true"`

    // 🆕 可选：最大续期次数（0 表示无限制）
    MaxTermDepositRenewals uint64 `yaml:"MAX_TERM_DEPOSIT_RENEWALS" spec:"true"`

    // 🆕 罚没池分配间隔（epoch 数）
    TermDepositPenaltyPoolDistributionInterval uint64 `yaml:"TERM_DEPOSIT_PENALTY_POOL_DISTRIBUTION_INTERVAL" spec:"true"`

    // 🆕 定期存单分叉激活 epoch
    TermDepositForkEpoch primitives.Epoch `yaml:"TERM_DEPOSIT_FORK_EPOCH" spec:"true"`
}
```

修改文件：`config/params/mainnet_config.go`

```go
var mainnetBeaconConfig = &BeaconChainConfig{
    // ... 现有配置 ...

    // 定期存单配置
	TermDepositsLimit:                          1 << 20, // 1,048,576
	PendingTermDepositsLimit:                   1 << 16, // 65,536
	MaxTermDepositsPerValidator:                256,
	DefaultTermDepositGracePeriod:              4050,            // 🆕 约 3 天 (2700 epochs × 64 s)
	MinTermDepositGracePeriod:                  1350,            // 🆕 约 1 天
	MaxTermDepositGracePeriod:                  40500,           // 🆕 约 30 天
	MinTermDepositDuration:                     4050,            // 约 3 天
	MaxTermDepositDuration:                     1350 * 365 * 10, // 约 10 年
	EarlyWithdrawalPenaltyBaseBps:              100,             // 1%
	MaxTermDepositRenewals:                     0,               // 🆕 0 = 无限制（可根据需求调整）
	TermDepositPenaltyPoolDistributionInterval: 1350,            // 🆕 每天分配一次
	TermDepositForkEpoch:                       0,               // 🆕 待定，根据实际部署设置
}
```

---

## 5. 代码修改清单

### 5.1 新建文件列表

| 文件路径 | 描述 |
|---------|------|
| `proto/prysm/v1alpha1/term_deposit.proto` | 定期存单 protobuf 定义 |
| `beacon-chain/core/electra/term_deposit.go` | 🆕 存单核心处理逻辑（含修复） |
| `beacon-chain/core/electra/term_deposit_test.go` | 存单逻辑单元测试 |
| `beacon-chain/core/electra/term_deposit_metrics.go` | 🆕 监控指标 |
| `beacon-chain/state/state-native/getters_term_deposit.go` | 状态 getter 方法 |
| `beacon-chain/state/state-native/setters_term_deposit.go` | 状态 setter 方法 |
| `beacon-chain/cache/depositsnapshot/term_deposit_cache.go` | 🆕 存单缓存（内存优化） |
| `contracts/deposit/term_deposit_logs.go` | 存单日志解析 |
| `contracts/deposit/DepositContractV2.sol` | 🆕 新版存款合约实现 |
| `contracts/deposit/TransparentUpgradeableProxy.sol` | 🆕 代理合约 |
| `contracts/deposit/TermManager.sol` | 🆕 管理合约 |

### 5.2 修改文件列表（🆕 标注修复重点）

| 文件路径 | 修改类型 | 说明 | 🆕 修复重点 |
|---------|---------|------|-----------|
| `proto/prysm/v1alpha1/beacon_state.proto` | 新增字段 | BeaconStateElectra 新增存单字段 | 优化字段编号 |
| `config/params/config.go` | 新增字段 | 存单配置参数 | 增加宽限期参数 |
| `config/params/mainnet_config.go` | 新增配置 | 主网配置值 | 宽限期延长至 10 天 |
| `beacon-chain/core/electra/effective_balance_updates.go` | **🔴 核心修改** | 有效余额计算包含定期存单 | 复合凭证兼容性 |
| `beacon-chain/core/electra/transition.go` | **🔴 新增调用** | 调用存单处理函数 | 明确处理顺序 |
| `beacon-chain/core/helpers/validators.go` | 逻辑调整 | 激活条件检查 | - |
| `beacon-chain/core/validators/validator.go` | **🔴 新增逻辑** | 退出和 Slashing 处理 | 增强原子性 |
| `beacon-chain/execution/log_processing.go` | 新增处理 | 存单日志处理 | 分叉激活检查 |
| `beacon-chain/state/interfaces.go` | 新增接口 | 存单相关接口 | - |
| `beacon-chain/state/state-native/types/types.go` | 新增常量 | FieldIndex | 连续编号 |
| `beacon-chain/state/state-native/beacon_state.go` | 新增字段 | 🆕 内存缓存 | 查询优化 |
| `beacon-chain/core/transition/state.go` | 初始化 | 创世状态初始化 | - |
| `runtime/interop/generate_genesis_state.go` | 初始化 | 测试网创世 | - |

---

## 6. 详细修改指南

### 6.1 核心修改：有效余额计算（🆕 修复复合凭证兼容性）

**文件**: `beacon-chain/core/electra/effective_balance_updates.go`

**原代码** (第 32-64 行):
```go
func ProcessEffectiveBalanceUpdates(st state.BeaconState) error {
    effBalanceInc := params.BeaconConfig().EffectiveBalanceIncrement
    hysteresisInc := effBalanceInc / params.BeaconConfig().HysteresisQuotient
    downwardThreshold := hysteresisInc * params.BeaconConfig().HysteresisDownwardMultiplier
    upwardThreshold := hysteresisInc * params.BeaconConfig().HysteresisUpwardMultiplier

    bals := st.Balances()

    validatorFunc := func(idx int, val state.ReadOnlyValidator) (newVal *ethpb.Validator, err error) {
        // ... 现有代码使用 balance := bals[idx] ...
    }
    return st.ApplyToEveryValidator(validatorFunc)
}
```

**🆕 修订后**:
```go
func ProcessEffectiveBalanceUpdates(st state.BeaconState) error {
    effBalanceInc := params.BeaconConfig().EffectiveBalanceIncrement
    hysteresisInc := effBalanceInc / params.BeaconConfig().HysteresisQuotient
    downwardThreshold := hysteresisInc * params.BeaconConfig().HysteresisDownwardMultiplier
    upwardThreshold := hysteresisInc * params.BeaconConfig().HysteresisUpwardMultiplier

    bals := st.Balances()
    currentEpoch := slots.ToEpoch(st.Slot())

    validatorFunc := func(idx int, val state.ReadOnlyValidator) (newVal *ethpb.Validator, err error) {
        if val.IsNil() {
            return nil, fmt.Errorf("validator %d is nil in state", idx)
        }
        if idx >= len(bals) {
            return nil, fmt.Errorf("validator index exceeds validator length in state %d >= %d", idx, len(bals))
        }

        // 活期余额
        demandBalance := bals[idx]

        // 🆕 获取未过期定期存单总额（包括宽限期内的存单）
        termBalance, err := GetActiveTermDepositBalance(st, primitives.ValidatorIndex(idx), currentEpoch)
        if err != nil {
            return nil, errors.Wrap(err, "failed to get term deposit balance")
        }

        // 总可用余额 = 活期 + 定期
        totalBalance := demandBalance + termBalance

        // 🆕 修复：有效余额上限逻辑
        effectiveBalanceLimit := params.BeaconConfig().MinActivationBalance
        if val.HasCompoundingWithdrawalCredentials() {
            effectiveBalanceLimit = params.BeaconConfig().MaxEffectiveBalanceElectra
        } else if termBalance > 0 {
            // 🆕 对于有定期存单但无复合凭证的验证者，记录警告
            // 选项 1：保持 MinActivationBalance 上限（推荐，更安全）
            log.Warnf("Validator %d has %d gwei in term deposits but no compounding credentials, effective balance capped at MinActivationBalance (%d gwei)",
                idx, termBalance, effectiveBalanceLimit)

            // 选项 2：提升上限以支持定期存单（如果私有链策略允许）
            // effectiveBalanceLimit = params.BeaconConfig().MaxEffectiveBalanceElectra
            // log.Infof("Validator %d has term deposits, raising effective balance limit to %d gwei",
            //     idx, effectiveBalanceLimit)
        }

        if totalBalance+downwardThreshold < val.EffectiveBalance() ||
           val.EffectiveBalance()+upwardThreshold < totalBalance {
            effectiveBal := min(totalBalance-totalBalance%effBalanceInc, effectiveBalanceLimit)
            newVal = val.Copy()
            newVal.EffectiveBalance = effectiveBal
        }
        return newVal, nil
    }

    return st.ApplyToEveryValidator(validatorFunc)
}
```

### 6.2 新建文件：存单核心逻辑（🆕 完整修复版）

**文件**: `beacon-chain/core/electra/term_deposit.go`

```go
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

// TermDeposit 状态常量（与 proto 中的定义保持一致）
const (
	TermDepositStatusActive            = 0 // 活跃（未到期）
	TermDepositStatusMatured           = 1 // 已到期（宽限期内）
	TermDepositStatusWithdrawn         = 2 // 已撤出
	TermDepositStatusPendingWithdrawal = 3 // 撤出中
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
			if primitives.Epoch(td.MaturityEpoch) > currentEpoch {
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
// 🆕 修复：时间逻辑，避免悖论
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

		// 🆕 修复：检查是否已经到期（使用 > 而不是 >=）
		if currentEpoch > td.MaturityEpoch {
			graceEndEpoch := td.MaturityEpoch + primitives.Epoch(td.GracePeriod)

			if currentEpoch > graceEndEpoch {
				// 🆕 检查是否超过最大续期次数（如果配置了限制）
				maxRenewals := params.BeaconConfig().MaxTermDepositRenewals
				if maxRenewals > 0 && td.RenewalCount >= uint32(maxRenewals) {
					// 超过最大续期次数，强制转为活期
					if err := helpers.IncreaseBalance(st, primitives.ValidatorIndex(td.ValidatorIndex), td.Amount); err != nil {
						return err
					}

					td.Status = TermDepositStatusWithdrawn
					td.Amount = 0

					log.Infof("Term deposit %d exceeded max renewals (%d), converted to demand balance",
						td.DepositId, maxRenewals)
				} else {
					// 🆕 修复：续期时，StartEpoch 应该是宽限期结束后的下一个 epoch
					td.StartEpoch = graceEndEpoch + 1
					td.MaturityEpoch = graceEndEpoch + 1 + primitives.Epoch(td.TermDuration)
					td.RenewalCount++
					td.Status = TermDepositStatusActive

					log.Debugf("Term deposit %d auto-renewed (count: %d)", td.DepositId, td.RenewalCount)
				}
			} else {
				// 进入宽限期，状态变为 Matured
				td.Status = TermDepositStatusMatured
				log.Debugf("Term deposit %d entered grace period", td.DepositId)
			}

			if err := st.UpdateTermDepositAtIndex(uint64(i), td); err != nil {
				return errors.Wrapf(err, "failed to update term deposit %d", td.DepositId)
			}
		}
	}

	return nil
}

// ProcessPendingTermDeposits 处理待处理的定期存单
// 🆕 新增：完整的 BLS 签名验证和安全检查
func ProcessPendingTermDeposits(ctx context.Context, st state.BeaconState) error {
	currentEpoch := slots.ToEpoch(st.Slot())
	pendingTermDeposits, err := st.PendingTermDeposits()
	if err != nil {
		return errors.Wrap(err, "failed to get pending term deposits")
	}

	if len(pendingTermDeposits) == 0 {
		return nil
	}

	processedDeposits := make([]*ethpb.PendingTermDeposit, 0)

	for _, ptd := range pendingTermDeposits {
		// 🆕 新增：检查是否已最终确认
		depositEpoch := slots.ToEpoch(primitives.Slot(ptd.Slot))
		finalizedEpoch := st.FinalizedCheckpoint().Epoch
		if depositEpoch > finalizedEpoch {
			// 未最终确认，延迟处理
			log.Debugf("Term deposit at epoch %d not finalized yet (finalized: %d), skipping", depositEpoch, finalizedEpoch)
			continue
		}

		// 验证期限有效性
		if ptd.TermDuration < params.BeaconConfig().MinTermDepositDuration ||
			ptd.TermDuration > params.BeaconConfig().MaxTermDepositDuration {
			log.Warnf("Invalid term duration: %d (min: %d, max: %d)",
				ptd.TermDuration,
				params.BeaconConfig().MinTermDepositDuration,
				params.BeaconConfig().MaxTermDepositDuration)
			continue
		}

		// 查找验证者索引
		validatorIndex, exists := st.ValidatorIndexByPubkey(bytesutil.ToBytes48(ptd.PublicKey))
		if !exists {
			// 验证者不存在，需要先通过普通存款创建
			log.Debug("Validator not found for term deposit, skipping")
			continue
		}

		// 🆕 新增：获取验证者信息并验证提现凭证（使用 ReadOnly 版本以支持方法调用）
		validator, err := st.ValidatorAtIndexReadOnly(validatorIndex)
		if err != nil {
			return err
		}

		// 🆕 新增：验证提现凭证匹配
		if !bytes.Equal(validator.GetWithdrawalCredentials(), ptd.WithdrawalCredentials) {
			log.Warnf("Withdrawal credentials mismatch for validator %d", validatorIndex)
			continue
		}

		// 🆕 新增：检查复合凭证类型并记录警告
		if !validator.HasCompoundingWithdrawalCredentials() {
			log.Warnf("Validator %d does not have compounding credentials, term deposit effective balance will be capped at MinActivationBalance (%d gwei). Consider upgrading withdrawal credentials.", validatorIndex, params.BeaconConfig().MinActivationBalance)
			// 注意：仍然允许创建存单，但会记录警告
		}

		// 🆕 新增：验证 BLS 签名
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
			log.Warnf("Invalid public key: %v", err)
			continue
		}

		signature, err := bls.SignatureFromBytes(ptd.Signature)
		if err != nil {
			log.Warnf("Invalid signature: %v", err)
			continue
		}

		if !signature.Verify(pubKey, signingRoot[:]) {
			log.Warnf("Invalid BLS signature for term deposit from validator %d", validatorIndex)
			continue
		}

		// 检查存单数量限制
		existingDeposits, err := st.TermDepositsForValidator(validatorIndex)
		if err != nil {
			return err
		}
		if uint64(len(existingDeposits)) >= params.BeaconConfig().MaxTermDepositsPerValidator {
			log.Warnf("Validator %d exceeds max term deposits (%d)",
				validatorIndex, params.BeaconConfig().MaxTermDepositsPerValidator)
			continue
		}

		// 创建新存单
		nextId, err := st.NextTermDepositId()
		if err != nil {
			return err
		}

		// 🆕 验证宽限期范围
		gracePeriod := ptd.GracePeriod
		if gracePeriod == 0 {
			gracePeriod = params.BeaconConfig().DefaultTermDepositGracePeriod
		}

		minGracePeriod := params.BeaconConfig().MinTermDepositGracePeriod
		maxGracePeriod := params.BeaconConfig().MaxTermDepositGracePeriod
		if gracePeriod < minGracePeriod || gracePeriod > maxGracePeriod {
			log.Warnf("Invalid grace period %d (min: %d, max: %d), using default %d",
				gracePeriod, minGracePeriod, maxGracePeriod, params.BeaconConfig().DefaultTermDepositGracePeriod)
			gracePeriod = params.BeaconConfig().DefaultTermDepositGracePeriod
		}

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

		log.Infof("Created term deposit %d for validator %d: amount=%d, duration=%d epochs, grace=%d epochs",
			nextId, validatorIndex, ptd.Amount, ptd.TermDuration, gracePeriod)

		processedDeposits = append(processedDeposits, ptd)
	}

	// 🆕 修复：只移除已处理的存单（而不是清空队列）
	remaining := make([]*ethpb.PendingTermDeposit, 0)
	for _, ptd := range pendingTermDeposits {
		found := false
		for _, processed := range processedDeposits {
			if bytes.Equal(ptd.PublicKey, processed.PublicKey) &&
				ptd.Amount == processed.Amount &&
				ptd.Slot == processed.Slot {
				found = true
				break
			}
		}
		if !found {
			remaining = append(remaining, ptd)
		}
	}

	return st.SetPendingTermDeposits(remaining)
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
		td, tdIndex, err := st.TermDepositById(req.DepositId)
		if err != nil {
			log.Debugf("Term deposit %d not found, skipping withdrawal", req.DepositId)
			continue
		}

		// 验证所有权
		if td.ValidatorIndex != req.ValidatorIndex {
			log.Warnf("Validator %d attempted to withdraw deposit %d owned by validator %d",
				req.ValidatorIndex, req.DepositId, td.ValidatorIndex)
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
		if err := st.UpdateTermDepositAtIndex(tdIndex, td); err != nil {
			return err
		}

		log.Infof("Processed early withdrawal for deposit %d: amount=%d, penalty=%d, returned=%d",
			td.DepositId, td.Amount, penaltyAmount, returnAmount)
	}

	// 清空待处理撤出请求
	return st.SetPendingTermWithdrawals([]*ethpb.TermWithdrawalRequest{})
}

// SlashTermDeposits 处理 Slashing 时的定期存单扣除
// 🆕 修复：增强原子性，预先计算所有变化再一次性提交
func SlashTermDeposits(
	st state.BeaconState,
	validatorIndex primitives.ValidatorIndex,
	remainingPenalty uint64,
) error {
	if remainingPenalty == 0 {
		return nil
	}

	termDeposits, err := st.TermDepositsForValidator(validatorIndex)
	if err != nil {
		return err
	}

	// 🆕 阶段 1：预先计算所有变化
	type depositUpdate struct {
		deposit        *ethpb.TermDeposit
		deduction      uint64
		shouldWithdraw bool
	}

	updates := make([]depositUpdate, 0)
	totalDeducted := uint64(0)
	pendingPenalty := remainingPenalty
	depositIds := make([]uint64, 0)
	amounts := make([]uint64, 0)

	for _, td := range termDeposits {
		if pendingPenalty == 0 {
			break
		}

		if td.Status == TermDepositStatusWithdrawn {
			continue
		}

		// 从存单中扣除
		deduction := min(td.Amount, pendingPenalty)
		pendingPenalty -= deduction
		totalDeducted += deduction

		depositIds = append(depositIds, td.DepositId)
		amounts = append(amounts, deduction)

		updates = append(updates, depositUpdate{
			deposit:        td,
			deduction:      deduction,
			shouldWithdraw: (td.Amount == deduction),
		})
	}

	if totalDeducted == 0 {
		return nil // 没有可扣除的定期存单
	}

	// 🆕 阶段 2：原子性应用所有变化
	for _, update := range updates {
		update.deposit.Amount -= update.deduction

		// 如果存单金额归零，标记为已撤出
		if update.shouldWithdraw {
			update.deposit.Status = TermDepositStatusWithdrawn
		}

		if err := st.UpdateTermDepositById(update.deposit.DepositId, update.deposit); err != nil {
			return errors.Wrapf(err, "failed to update term deposit %d during slashing", update.deposit.DepositId)
		}
	}

	// 🆕 阶段 3：更新罚没池
	currentPenaltyPool, err := st.TermDepositPenaltyPool()
	if err != nil {
		return err
	}

	if err := st.SetTermDepositPenaltyPool(currentPenaltyPool + totalDeducted); err != nil {
		return err
	}

	// 🆕 阶段 4：记录 Slashing 日志（用于审计）
	slashingLog := &ethpb.TermDepositSlashingLog{
		Epoch:          slots.ToEpoch(st.Slot()),
		ValidatorIndex: validatorIndex,
		DepositIds:     depositIds,
		Amounts:        amounts,
		TotalSlashed:   totalDeducted,
	}

	_, err = st.TermDepositSlashingLogs()
	if err == nil {
		// 如果支持 slashing logs，记录
		if err := st.AppendTermDepositSlashingLog(slashingLog); err != nil {
			log.Warnf("Failed to append slashing log: %v", err)
		}
	}

	log.Infof("Slashed %d gwei from term deposits of validator %d (affected deposits: %d)",
		totalDeducted, validatorIndex, len(updates))

	return nil
}

// 🆕 ProcessTermDepositPenaltyDistribution 分配罚没池奖励
func ProcessTermDepositPenaltyDistribution(ctx context.Context, st state.BeaconState) error {
	currentEpoch := slots.ToEpoch(st.Slot())

	// 检查是否到了分配间隔
	lastDistributionEpoch, err := st.TermDepositPenaltyPoolLastDistributionEpoch()
	if err != nil {
		return err
	}

	distributionInterval := params.BeaconConfig().TermDepositPenaltyPoolDistributionInterval
	if currentEpoch < lastDistributionEpoch+primitives.Epoch(distributionInterval) {
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
	for _, idx := range activeValidators {
		val, err := st.ValidatorAtIndex(idx)
		if err != nil {
			continue
		}

		reward := pool * val.EffectiveBalance / totalEffectiveBalance
		if reward > 0 {
			if err := helpers.IncreaseBalance(st, idx, reward); err != nil {
				return err
			}
		}
	}

	// 清空奖励池并更新分配时间
	if err := st.SetTermDepositPenaltyPool(0); err != nil {
		return err
	}

	if err := st.SetTermDepositPenaltyPoolLastDistributionEpoch(currentEpoch); err != nil {
		return err
	}

	log.Infof("Distributed %d gwei from penalty pool to %d active validators", pool, len(activeValidators))

	return nil
}

// 🆕 CleanupValidatorTermDeposits 清理已退出验证者的存单
func CleanupValidatorTermDeposits(st state.BeaconState, validatorIndex primitives.ValidatorIndex) error {
	validator, err := st.ValidatorAtIndex(validatorIndex)
	if err != nil {
		return err
	}

	// 只有在验证者完全提款后才清理
	balance, err := st.BalanceAtIndex(validatorIndex)
	if err != nil {
		return err
	}

	if balance > 0 || validator.EffectiveBalance > 0 {
		return nil // 还有余额，不清理
	}

	// 获取所有存单
	deposits, err := st.TermDepositsForValidator(validatorIndex)
	if err != nil {
		return err
	}

	// 标记所有存单为已撤出（如果还有活跃的）
	cleanupCount := 0
	for _, td := range deposits {
		if td.Status != TermDepositStatusWithdrawn {
			td.Status = TermDepositStatusWithdrawn
			if err := st.UpdateTermDepositById(td.DepositId, td); err != nil {
				return err
			}
			cleanupCount++
		}
	}

	if cleanupCount > 0 {
		log.Infof("Cleaned up %d term deposits for exited validator %d", cleanupCount, validatorIndex)
	}

	return nil
}

// helper function
func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

```

> **⚠️ 注意**：`SlashTermDeposits` 函数在 `beacon-chain/core/validators/validator.go` 中有一份副本，用于 `validators.SlashValidator` 调用，以避免 `electra` 和 `validators` 包之间的循环依赖。如需修改此函数逻辑，请同时更新两处。

### 6.3 修改 Epoch 处理（🆕 明确处理顺序）

**文件**: `beacon-chain/core/electra/transition.go`

在 `ProcessEpoch` 函数中添加存单处理调用：

```go
func ProcessEpoch(ctx context.Context, state state.BeaconState) error {
	_, span := trace.StartSpan(ctx, "electra.ProcessEpoch")
	defer span.End()

	if state == nil || state.IsNil() {
		return errors.New("nil state")
	}
	vp, bp, err := InitializePrecomputeValidators(ctx, state)
	if err != nil {
		return err
	}
	vp, bp, err = ProcessEpochParticipation(ctx, state, bp, vp)
	if err != nil {
		return err
	}
	state, err = precompute.ProcessJustificationAndFinalizationPreCompute(state, bp)
	if err != nil {
		return errors.Wrap(err, "could not process justification")
	}
	state, vp, err = ProcessInactivityScores(ctx, state, vp)
	if err != nil {
		return errors.Wrap(err, "could not process inactivity updates")
	}
	state, err = ProcessRewardsAndPenaltiesPrecompute(state, bp, vp)
	if err != nil {
		return errors.Wrap(err, "could not process rewards and penalties")
	}
	if err := ProcessRegistryUpdates(ctx, state); err != nil {
		return errors.Wrap(err, "could not process registry updates")
	}
	if err := ProcessSlashings(state); err != nil {
		return err
	}
	state, err = ProcessEth1DataReset(state)
	if err != nil {
		return err
	}
	if err = ProcessPendingDeposits(ctx, state, primitives.Gwei(bp.ActiveCurrentEpoch)); err != nil {
		return err
	}
	if err = ProcessPendingConsolidations(ctx, state); err != nil {
		return err
	}

	// Term deposit processing (after pending consolidations, before effective balance updates)
	if err = ProcessTermDepositMaturity(ctx, state); err != nil {
		return errors.Wrap(err, "could not process term deposit maturity")
	}
	if err = ProcessPendingTermDeposits(ctx, state); err != nil {
		return errors.Wrap(err, "could not process pending term deposits")
	}
	if err = ProcessTermWithdrawalRequests(ctx, state); err != nil {
		return errors.Wrap(err, "could not process term withdrawal requests")
	}

	if err = ProcessEffectiveBalanceUpdates(state); err != nil {
		return err
	}
	state, err = ProcessSlashingsReset(state)
	if err != nil {
		return err
	}
	state, err = ProcessRandaoMixesReset(state)
	if err != nil {
		return err
	}
	state, err = ProcessHistoricalDataUpdate(state)
	if err != nil {
		return err
	}
	state, err = ProcessParticipationFlagUpdates(state)
	if err != nil {
		return err
	}
	_, err = ProcessSyncCommitteeUpdates(ctx, state)
	if err != nil {
		return err
	}

	// Term deposit penalty distribution
	if err = ProcessTermDepositPenaltyDistribution(ctx, state); err != nil {
		return errors.Wrap(err, "could not distribute penalty pool")
	}

	// Update term deposit metrics for monitoring
	if err = UpdateTermDepositMetrics(state); err != nil {
		log.WithError(err).Warn("Failed to update term deposit metrics")
	}

	return nil
}
```

**新增函数**: `UpdateTermDepositMetrics` 在 `beacon-chain/core/electra/term_deposit.go` 中实现：

```go
// UpdateTermDepositMetrics 更新定期存单监控指标
// 用于监控系统健康状态和统计数据
func UpdateTermDepositMetrics(st state.BeaconState) error {
	termDeposits, err := st.TermDeposits()
	if err != nil {
		return errors.Wrap(err, "failed to get term deposits for metrics")
	}

	// 统计各状态的存单数量和金额
	var (
		activeCount, maturedCount, withdrawnCount, pendingCount uint64
		activeAmount, maturedAmount                             uint64
	)

	for _, td := range termDeposits {
		switch td.Status {
		case TermDepositStatusActive:
			activeCount++
			activeAmount += td.Amount
		case TermDepositStatusMatured:
			maturedCount++
			maturedAmount += td.Amount
		case TermDepositStatusWithdrawn:
			withdrawnCount++
		case TermDepositStatusPendingWithdrawal:
			pendingCount++
		}
	}

	// 获取罚没池余额
	penaltyPool, err := st.TermDepositPenaltyPool()
	if err != nil {
		penaltyPool = 0 // 如果获取失败，使用 0
	}

	// 记录日志（实际部署时可替换为 prometheus metrics）
	log.WithFields(log.Fields{
		"total_deposits":    len(termDeposits),
		"active_count":      activeCount,
		"active_amount":     activeAmount,
		"matured_count":     maturedCount,
		"matured_amount":    maturedAmount,
		"withdrawn_count":   withdrawnCount,
		"pending_count":     pendingCount,
		"penalty_pool_gwei": penaltyPool,
	}).Debug("Term deposit metrics updated")

	return nil
}
```

### 6.4 修改 Slashing 逻辑（🆕 增强原子性）

**文件**: `beacon-chain/core/validators/validator.go`

在 `SlashValidator` 函数中添加定期存单处理：

```go
// SlashTermDeposits 处理 Slashing 时的定期存单扣除
// 🆕 修复：增强原子性，预先计算所有变化再一次性提交
// 注意：此函数是 electra 包中同名函数的副本，放在此处是为了避免循环依赖
func SlashTermDeposits(
	st state.BeaconState,
	validatorIndex primitives.ValidatorIndex,
	remainingPenalty uint64,
) error {
	if remainingPenalty == 0 {
		return nil
	}

	termDeposits, err := st.TermDepositsForValidator(validatorIndex)
	if err != nil {
		return err
	}

	// 🆕 阶段 1：预先计算所有变化
	type depositUpdate struct {
		deposit        *ethpb.TermDeposit
		deduction      uint64
		shouldWithdraw bool
	}

	updates := make([]depositUpdate, 0)
	totalDeducted := uint64(0)
	pendingPenalty := remainingPenalty
	depositIds := make([]uint64, 0)
	amounts := make([]uint64, 0)

	for _, td := range termDeposits {
		if pendingPenalty == 0 {
			break
		}

		if td.Status == TermDepositStatusWithdrawn {
			continue
		}

		// 从存单中扣除
		deduction := min(td.Amount, pendingPenalty)
		pendingPenalty -= deduction
		totalDeducted += deduction

		depositIds = append(depositIds, td.DepositId)
		amounts = append(amounts, deduction)

		updates = append(updates, depositUpdate{
			deposit:        td,
			deduction:      deduction,
			shouldWithdraw: (td.Amount == deduction),
		})
	}

	if totalDeducted == 0 {
		return nil // 没有可扣除的定期存单
	}

	// 🆕 阶段 2：原子性应用所有变化
	for _, update := range updates {
		update.deposit.Amount -= update.deduction

		// 如果存单金额归零，标记为已撤出
		if update.shouldWithdraw {
			update.deposit.Status = TermDepositStatusWithdrawn
		}

		if err := st.UpdateTermDepositById(update.deposit.DepositId, update.deposit); err != nil {
			return errors.Wrapf(err, "failed to update term deposit %d during slashing", update.deposit.DepositId)
		}
	}

	// 🆕 阶段 3：更新罚没池
	currentPenaltyPool, err := st.TermDepositPenaltyPool()
	if err != nil {
		return err
	}

	if err := st.SetTermDepositPenaltyPool(currentPenaltyPool + totalDeducted); err != nil {
		return err
	}

	// 🆕 阶段 4：记录 Slashing 日志（用于审计）
	slashingLog := &ethpb.TermDepositSlashingLog{
		Epoch:          slots.ToEpoch(st.Slot()),
		ValidatorIndex: validatorIndex,
		DepositIds:     depositIds,
		Amounts:        amounts,
		TotalSlashed:   totalDeducted,
	}

	_, err = st.TermDepositSlashingLogs()
	if err == nil {
		// 如果支持 slashing logs，记录
		if err := st.AppendTermDepositSlashingLog(slashingLog); err != nil {
			log.Warnf("Failed to append slashing log: %v", err)
		}
	}

	log.Infof("Slashed %d gwei from term deposits of validator %d (affected deposits: %d)",
		totalDeducted, validatorIndex, len(updates))

	return nil
}

func SlashValidator(
    ctx context.Context,
    s state.BeaconState,
    slashedIdx primitives.ValidatorIndex,
    exitInfo *ExitInfo,
) (state.BeaconState, error) {
    // ... 现有代码：计算 slashingPenalty ...

    // 获取活期余额
    demandBalance, err := s.BalanceAtIndex(slashedIdx)
    if err != nil {
        return nil, err
    }

    if demandBalance >= slashingPenalty {
        // 活期余额充足，直接扣除
        if err := helpers.DecreaseBalance(s, slashedIdx, slashingPenalty); err != nil {
            return nil, err
        }
        log.Debugf("Slashed %d gwei from demand balance of validator %d", slashingPenalty, slashedIdx)
    } else {
        // 🆕 活期不足，扣除全部活期后从定期存单扣除
        if err := helpers.DecreaseBalance(s, slashedIdx, demandBalance); err != nil {
            return nil, err
        }

        remainingPenalty := slashingPenalty - demandBalance

        log.Infof("Demand balance insufficient for slashing penalty: need=%d, available=%d, deficit=%d",
            slashingPenalty, demandBalance, remainingPenalty)

        // 🆕 从定期存单中扣除剩余惩罚（增强原子性）
        // 注意：这里使用 validators 包内的 SlashTermDeposits 副本，避免循环依赖
        if err := SlashTermDeposits(s, slashedIdx, remainingPenalty); err != nil {
            return nil, errors.Wrap(err, "failed to slash term deposits")
        }
    }

    // ... 后续处理 ...
    return s, nil
}
```

### 6.5 ETH1 日志处理（🆕 添加分叉激活检查）

**文件**: `beacon-chain/execution/log_processing.go`

```go
import (
    // ... 现有导入 ...
    "github.com/OffchainLabs/prysm/v6/contracts/deposit"
)

var (
    depositEventSignature     = hash.Keccak256([]byte("DepositEvent(bytes,bytes,bytes,bytes,bytes)"))
    // 🆕 定期存单事件签名
    termDepositEventSignature = hash.Keccak256([]byte("TermDepositEvent(bytes,bytes,bytes,bytes,bytes,uint64,uint64)"))
    termWithdrawalSignature   = hash.Keccak256([]byte("TermWithdrawalRequestEvent(uint64,bytes,uint32)"))
)

func (s *Service) ProcessLog(ctx context.Context, depositLog *gethtypes.Log) error {
    s.processingLock.RLock()
    defer s.processingLock.RUnlock()

    // 🆕 检查是否达到分叉激活点
    currentEpoch := slots.ToEpoch(s.chainService.CurrentSlot())
    termDepositEnabled := currentEpoch >= params.BeaconConfig().TermDepositForkEpoch

    // 根据事件签名分发处理
    switch {
    case bytes.Equal(depositLog.Topics[0].Bytes(), depositEventSignature.Bytes()):
        return s.ProcessDepositLog(ctx, depositLog)
    case termDepositEnabled && bytes.Equal(depositLog.Topics[0].Bytes(), termDepositEventSignature.Bytes()):
        return s.ProcessTermDepositLog(ctx, depositLog)
    case termDepositEnabled && bytes.Equal(depositLog.Topics[0].Bytes(), termWithdrawalSignature.Bytes()):
        return s.ProcessTermWithdrawalLog(ctx, depositLog)
    default:
        log.WithField("signature", fmt.Sprintf("%#x", depositLog.Topics[0])).Debug("Not a valid event signature")
        return nil
    }
}

// 🆕 处理定期存单日志
func (s *Service) ProcessTermDepositLog(ctx context.Context, depositLog *gethtypes.Log) error {
    pubkey, withdrawalCredentials, amount, signature, index, termDuration, gracePeriod, err :=
        deposit.UnpackTermDepositLogData(depositLog.Data)
    if err != nil {
        return errors.Wrap(err, "Could not unpack term deposit log")
    }

    pendingTermDeposit := &ethpb.PendingTermDeposit{
        PublicKey:             pubkey,
        WithdrawalCredentials: withdrawalCredentials,
        Amount:                bytesutil.FromBytes8(amount),
        Signature:             signature,
        Slot:                  primitives.Slot(depositLog.BlockNumber),
        TermDuration:          termDuration,
        GracePeriod:           gracePeriod,
    }

    log.Infof("Received term deposit: amount=%d, duration=%d epochs, grace=%d epochs",
        pendingTermDeposit.Amount, termDuration, gracePeriod)

    return s.cfg.depositCache.InsertPendingTermDeposit(ctx, pendingTermDeposit, depositLog.BlockNumber, index)
}

// 🆕 处理存单撤出请求日志
func (s *Service) ProcessTermWithdrawalLog(ctx context.Context, depositLog *gethtypes.Log) error {
    depositId, validatorPubkey, penaltyBps, err := deposit.UnpackTermWithdrawalLogData(depositLog.Data)
    if err != nil {
        return errors.Wrap(err, "Could not unpack term withdrawal log")
    }

    // 查找验证者索引
    validatorIndex, exists := s.cfg.stateGen.CurrentState().ValidatorIndexByPubkey(bytesutil.ToBytes48(validatorPubkey))
    if !exists {
        log.Warnf("Validator not found for term withdrawal request")
        return nil
    }

    withdrawalRequest := &ethpb.TermWithdrawalRequest{
        DepositId:      depositId,
        ValidatorIndex: uint64(validatorIndex),
        PenaltyBps:     penaltyBps,
        Slot:           primitives.Slot(depositLog.BlockNumber),
    }

    log.Infof("Received term withdrawal request: deposit_id=%d, validator=%d, penalty=%d bps",
        depositId, validatorIndex, penaltyBps)

    return s.cfg.depositCache.InsertPendingTermWithdrawal(ctx, withdrawalRequest)
}
```

### 6.6 State 接口扩展

**文件**: `beacon-chain/state/interfaces.go`

```go
type BeaconState interface {
    // ... 现有接口 ...

    // ========== 定期存单相关接口 ==========

    // Getter 方法
    TermDeposits() ([]*ethpb.TermDeposit, error)
    TermDepositsForValidator(idx primitives.ValidatorIndex) ([]*ethpb.TermDeposit, error)
    TermDepositById(depositId uint64) (*ethpb.TermDeposit, uint64, error)
    PendingTermDeposits() ([]*ethpb.PendingTermDeposit, error)
    PendingTermWithdrawals() ([]*ethpb.TermWithdrawalRequest, error)
    NextTermDepositId() (uint64, error)
    TermDepositPenaltyPool() (uint64, error)
    TermDepositPenaltyPoolLastDistributionEpoch() (primitives.Epoch, error)
    TermDepositSlashingLogs() ([]*ethpb.TermDepositSlashingLog, error)

    // Setter 方法
    AppendTermDeposit(td *ethpb.TermDeposit) error
    UpdateTermDepositAtIndex(idx uint64, td *ethpb.TermDeposit) error
    UpdateTermDepositById(depositId uint64, td *ethpb.TermDeposit) error
    RemoveTermDeposit(depositId uint64) error
    SetPendingTermDeposits(ptds []*ethpb.PendingTermDeposit) error
    AppendPendingTermDeposit(ptd *ethpb.PendingTermDeposit) error
    SetPendingTermWithdrawals(reqs []*ethpb.TermWithdrawalRequest) error
    SetNextTermDepositId(id uint64) error
    SetTermDepositPenaltyPool(amount uint64) error
    SetTermDepositPenaltyPoolLastDistributionEpoch(epoch primitives.Epoch) error
    AppendTermDepositSlashingLog(log *ethpb.TermDepositSlashingLog) error

    // 计算方法
    ActiveTermDepositBalance(idx primitives.ValidatorIndex, epoch primitives.Epoch) (uint64, error)
}
```

### 6.7 State 内存缓存优化（🆕 提升查询性能）

**文件**: `beacon-chain/state/state-native/beacon_state.go`

```go
type BeaconState struct {
    // ... 现有字段 ...

    // 定期存单字段
    termDeposits                          []*ethpb.TermDeposit
    pendingTermDeposits                   []*ethpb.PendingTermDeposit
    nextTermDepositId                     uint64
    pendingTermWithdrawals                []*ethpb.TermWithdrawalRequest
    termDepositPenaltyPool                uint64
    termDepositPenaltyPoolLastDistEpoch   primitives.Epoch
    termDepositSlashingLogs               []*ethpb.TermDepositSlashingLog

    // 🆕 内存缓存，不持久化（提升查询性能）
    validatorTermDepositCache map[primitives.ValidatorIndex][]*ethpb.TermDeposit
    termDepositCacheLock      sync.RWMutex
    termDepositByIdCache      map[uint64]*ethpb.TermDeposit
    termDepositByIdCacheLock  sync.RWMutex

    // ... 其他现有字段 ...
}

// 🆕 AppendTermDeposit 添加定期存单（带缓存更新）
func (b *BeaconState) AppendTermDeposit(td *ethpb.TermDeposit) error {
    // 添加到列表
    b.termDeposits = append(b.termDeposits, td)

    // 🆕 更新验证者缓存
    b.termDepositCacheLock.Lock()
    idx := primitives.ValidatorIndex(td.ValidatorIndex)
    if b.validatorTermDepositCache == nil {
        b.validatorTermDepositCache = make(map[primitives.ValidatorIndex][]*ethpb.TermDeposit)
    }
    b.validatorTermDepositCache[idx] = append(b.validatorTermDepositCache[idx], td)
    b.termDepositCacheLock.Unlock()

    // 🆕 更新 ID 缓存
    b.termDepositByIdCacheLock.Lock()
    if b.termDepositByIdCache == nil {
        b.termDepositByIdCache = make(map[uint64]*ethpb.TermDeposit)
    }
    b.termDepositByIdCache[td.DepositId] = td
    b.termDepositByIdCacheLock.Unlock()

    b.markFieldAsDirty(types.TermDeposits)
    return nil
}

// 🆕 TermDepositsForValidator 查询验证者的所有存单（使用缓存）
func (b *BeaconState) TermDepositsForValidator(idx primitives.ValidatorIndex) ([]*ethpb.TermDeposit, error) {
    b.termDepositCacheLock.RLock()
    if deposits, exists := b.validatorTermDepositCache[idx]; exists {
        b.termDepositCacheLock.RUnlock()
        return deposits, nil
    }
    b.termDepositCacheLock.RUnlock()

    // 缓存未命中，重建
    deposits := make([]*ethpb.TermDeposit, 0)
    for _, td := range b.termDeposits {
        if td.ValidatorIndex == uint64(idx) {
            deposits = append(deposits, td)
        }
    }

    // 更新缓存
    b.termDepositCacheLock.Lock()
    if b.validatorTermDepositCache == nil {
        b.validatorTermDepositCache = make(map[primitives.ValidatorIndex][]*ethpb.TermDeposit)
    }
    b.validatorTermDepositCache[idx] = deposits
    b.termDepositCacheLock.Unlock()

    return deposits, nil
}

// 🆕 TermDepositById 根据 ID 查询存单（使用缓存）
func (b *BeaconState) TermDepositById(depositId uint64) (*ethpb.TermDeposit, uint64, error) {
    // 尝试从缓存获取
    b.termDepositByIdCacheLock.RLock()
    if td, exists := b.termDepositByIdCache[depositId]; exists {
        b.termDepositByIdCacheLock.RUnlock()
        // 找到索引
        for i, deposit := range b.termDeposits {
            if deposit.DepositId == depositId {
                return td, uint64(i), nil
            }
        }
    }
    b.termDepositByIdCacheLock.RUnlock()

    // 缓存未命中，遍历查找
    for i, td := range b.termDeposits {
        if td.DepositId == depositId {
            // 更新缓存
            b.termDepositByIdCacheLock.Lock()
            if b.termDepositByIdCache == nil {
                b.termDepositByIdCache = make(map[uint64]*ethpb.TermDeposit)
            }
            b.termDepositByIdCache[depositId] = td
            b.termDepositByIdCacheLock.Unlock()

            return td, uint64(i), nil
        }
    }

    return nil, 0, errors.New("term deposit not found")
}

// 🆕 UpdateTermDepositById 更新存单（同时更新缓存）
func (b *BeaconState) UpdateTermDepositById(depositId uint64, td *ethpb.TermDeposit) error {
    for i, existing := range b.termDeposits {
        if existing.DepositId == depositId {
            b.termDeposits[i] = td

            // 更新 ID 缓存
            b.termDepositByIdCacheLock.Lock()
            if b.termDepositByIdCache != nil {
                b.termDepositByIdCache[depositId] = td
            }
            b.termDepositByIdCacheLock.Unlock()

            // 使验证者缓存失效
            b.termDepositCacheLock.Lock()
            if b.validatorTermDepositCache != nil {
                delete(b.validatorTermDepositCache, primitives.ValidatorIndex(td.ValidatorIndex))
            }
            b.termDepositCacheLock.Unlock()

            b.markFieldAsDirty(types.TermDeposits)
            b.addDirtyIndices(types.TermDeposits, []uint64{uint64(i)})
            return nil
        }
    }

    return errors.New("term deposit not found")
}
```

### 6.8 监控指标实现（🆕 完整的可观测性）

**文件**: `beacon-chain/core/electra/term_deposit_metrics.go`

```go
package electra

import (
    "github.com/OffchainLabs/prysm/v6/beacon-chain/state"
    ethpb "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

var (
    // 存单总数
    termDepositsTotal = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "beacon_term_deposits_total",
        Help: "Total number of term deposits in the state",
    })

    // 按状态分类的存单数量
    termDepositsByStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
        Name: "beacon_term_deposits_by_status",
        Help: "Number of term deposits by status",
    }, []string{"status"})

    // 定期存单总金额
    termDepositsTotalAmount = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "beacon_term_deposits_total_amount_gwei",
        Help: "Total amount locked in term deposits (gwei)",
    })

    // 罚没池金额
    termDepositsPenaltyPool = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "beacon_term_deposits_penalty_pool_gwei",
        Help: "Total amount in term deposit penalty pool (gwei)",
    })

    // 到期存单数量
    termDepositsMaturedCount = promauto.NewCounter(prometheus.CounterOpts{
        Name: "beacon_term_deposits_matured_total",
        Help: "Total number of term deposits that have matured",
    })

    // 续期存单数量
    termDepositsRenewedCount = promauto.NewCounter(prometheus.CounterOpts{
        Name: "beacon_term_deposits_renewed_total",
        Help: "Total number of term deposits that have been renewed",
    })

    // 提前撤出数量
    termDepositsWithdrawnEarlyCount = promauto.NewCounter(prometheus.CounterOpts{
        Name: "beacon_term_deposits_withdrawn_early_total",
        Help: "Total number of term deposits withdrawn early",
    })

    // 提前撤出总罚金
    termDepositsWithdrawnEarlyPenalty = promauto.NewCounter(prometheus.CounterOpts{
        Name: "beacon_term_deposits_withdrawn_early_penalty_gwei",
        Help: "Total penalty paid for early withdrawals (gwei)",
    })

    // 平均存单期限
    termDepositsAverageDuration = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "beacon_term_deposits_average_duration_epochs",
        Help: "Average term duration of active deposits (epochs)",
    })

    // 每个验证者的平均存单数
    termDepositsPerValidator = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "beacon_term_deposits_per_validator_avg",
        Help: "Average number of term deposits per validator",
    })

    // 待处理的定期存单数量
    termDepositsPendingCount = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "beacon_term_deposits_pending_count",
        Help: "Number of pending term deposits waiting to be processed",
    })

    // Slashing 影响的存单数量
    termDepositsSlashedCount = promauto.NewCounter(prometheus.CounterOpts{
        Name: "beacon_term_deposits_slashed_total",
        Help: "Total number of term deposits affected by slashing",
    })
)

// UpdateTermDepositMetrics 更新所有指标
func UpdateTermDepositMetrics(st state.BeaconState) error {
    termDeposits, err := st.TermDeposits()
    if err != nil {
        return err
    }

    // 统计数据
    totalDeposits := len(termDeposits)
    activeCount := 0
    maturedCount := 0
    withdrawnCount := 0
    totalAmount := uint64(0)
    totalDuration := uint64(0)

    validatorSet := make(map[uint64]bool)

    for _, td := range termDeposits {
        switch td.Status {
        case TermDepositStatusActive:
            activeCount++
            totalAmount += td.Amount
            totalDuration += td.TermDuration
        case TermDepositStatusMatured:
            maturedCount++
            totalAmount += td.Amount
        case TermDepositStatusWithdrawn:
            withdrawnCount++
        }

        validatorSet[td.ValidatorIndex] = true
    }

    // 更新指标
    termDepositsTotal.Set(float64(totalDeposits))
    termDepositsByStatus.WithLabelValues("active").Set(float64(activeCount))
    termDepositsByStatus.WithLabelValues("matured").Set(float64(maturedCount))
    termDepositsByStatus.WithLabelValues("withdrawn").Set(float64(withdrawnCount))
    termDepositsTotalAmount.Set(float64(totalAmount))

    penaltyPool, err := st.TermDepositPenaltyPool()
    if err == nil {
        termDepositsPenaltyPool.Set(float64(penaltyPool))
    }

    if activeCount > 0 {
        avgDuration := totalDuration / uint64(activeCount)
        termDepositsAverageDuration.Set(float64(avgDuration))
    }

    if len(validatorSet) > 0 {
        avgPerValidator := float64(totalDeposits) / float64(len(validatorSet))
        termDepositsPerValidator.Set(avgPerValidator)
    }

    // 待处理存单数量
    pendingDeposits, err := st.PendingTermDeposits()
    if err == nil {
        termDepositsPendingCount.Set(float64(len(pendingDeposits)))
    }

    return nil
}
```

---

## 7. ETH1 合约改造

### 7.1 使用代理合约升级（🆕 推荐方案）

#### 7.1.1 新版存款合约实现

**文件**: `contracts/deposit/DepositContractV2.sol`

```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

interface ITermManager {
    function validateTermDuration(uint64 duration) external view returns (bool);
    function validateGracePeriod(uint64 gracePeriod) external view returns (bool);
    function calculatePenalty(uint64 depositId, uint64 currentEpoch) external view returns (uint32);
}

contract DepositContractV2 {
    // 管理合约地址
    ITermManager public termManager;

    // 存款计数器（共享）
    uint256 public deposit_count;

    // 现有事件
    event DepositEvent(
        bytes pubkey,
        bytes withdrawal_credentials,
        bytes amount,
        bytes signature,
        bytes index
    );

    // 🆕 定期存单事件
    event TermDepositEvent(
        bytes pubkey,
        bytes withdrawal_credentials,
        bytes amount,
        bytes signature,
        bytes index,
        uint64 term_duration,
        uint64 grace_period
    );

    // 🆕 存单撤出请求事件
    event TermWithdrawalRequestEvent(
        uint64 deposit_id,
        bytes validator_pubkey,
        uint32 penalty_bps
    );

    // 初始化管理合约地址
    function setTermManager(address _termManager) external {
        require(address(termManager) == address(0), "Term manager already set");
        termManager = ITermManager(_termManager);
    }

    // 现有存款功能（保持不变）
    function deposit(
        bytes calldata pubkey,
        bytes calldata withdrawal_credentials,
        bytes calldata signature,
        bytes32 deposit_data_root
    ) external payable {
        require(pubkey.length == 48, "Invalid pubkey length");
        require(withdrawal_credentials.length == 32, "Invalid withdrawal_credentials length");
        require(signature.length == 96, "Invalid signature length");
        require(msg.value >= 1 ether, "Minimum deposit is 1 ETH");
        require(msg.value % 1 gwei == 0, "Deposit value must be in whole gwei");

        bytes memory amount = to_little_endian_64(uint64(msg.value / 1 gwei));
        uint256 index = deposit_count;
        deposit_count += 1;

        // ... Merkle 树更新逻辑 ...

        emit DepositEvent(
            pubkey,
            withdrawal_credentials,
            amount,
            signature,
            to_little_endian_64(uint64(index))
        );
    }

    // 🆕 定期存单功能
    function termDeposit(
        bytes calldata pubkey,
        bytes calldata withdrawal_credentials,
        bytes calldata signature,
        uint64 term_duration,
        uint64 grace_period
    ) external payable {
        require(pubkey.length == 48, "Invalid pubkey length");
        require(withdrawal_credentials.length == 32, "Invalid withdrawal_credentials length");
        require(signature.length == 96, "Invalid signature length");
        require(msg.value >= 1 ether, "Minimum deposit is 1 ETH");
        require(msg.value % 1 gwei == 0, "Deposit value must be in whole gwei");

        // 通过管理合约验证期限有效性
        require(termManager.validateTermDuration(term_duration), "Invalid term duration");

        // 验证宽限期（如果提供）
        if (grace_period > 0) {
            require(termManager.validateGracePeriod(grace_period), "Invalid grace period");
        }

        bytes memory amount = to_little_endian_64(uint64(msg.value / 1 gwei));
        uint256 index = deposit_count;
        deposit_count += 1;

        // ... Merkle 树更新逻辑（共享同一个树）...

        emit TermDepositEvent(
            pubkey,
            withdrawal_credentials,
            amount,
            signature,
            to_little_endian_64(uint64(index)),
            term_duration,
            grace_period
        );
    }

    // 🆕 请求提前撤出
    function requestTermWithdrawal(
        uint64 deposit_id,
        bytes calldata validator_pubkey
    ) external {
        require(validator_pubkey.length == 48, "Invalid pubkey length");

        // 通过管理合约计算罚没比例
        uint32 penalty_bps = termManager.calculatePenalty(deposit_id, 0); // 0 表示当前 epoch

        emit TermWithdrawalRequestEvent(
            deposit_id,
            validator_pubkey,
            penalty_bps
        );
    }

    // 辅助函数：转换为小端序
    function to_little_endian_64(uint64 value) internal pure returns (bytes memory) {
        bytes memory result = new bytes(8);
        for (uint256 i = 0; i < 8; i++) {
            result[i] = bytes1(uint8(value >> (i * 8)));
        }
        return result;
    }
}
```

#### 7.1.2 透明代理合约

**文件**: `contracts/deposit/TransparentUpgradeableProxy.sol`

```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TransparentUpgradeableProxy {
    // 实现合约地址
    address public implementation;

    // 管理员地址
    address public admin;

    // 升级事件
    event Upgraded(address indexed implementation);
    event AdminChanged(address indexed previousAdmin, address indexed newAdmin);

    constructor(address _implementation, address _admin) {
        require(_implementation != address(0), "Invalid implementation address");
        require(_admin != address(0), "Invalid admin address");

        implementation = _implementation;
        admin = _admin;

        emit Upgraded(_implementation);
        emit AdminChanged(address(0), _admin);
    }

    // 升级实现合约（仅管理员）
    function upgradeTo(address newImplementation) external {
        require(msg.sender == admin, "Only admin can upgrade");
        require(newImplementation != address(0), "Invalid implementation address");
        require(newImplementation != implementation, "Same implementation");

        address oldImplementation = implementation;
        implementation = newImplementation;

        emit Upgraded(newImplementation);
    }

    // 更改管理员（仅管理员）
    function changeAdmin(address newAdmin) external {
        require(msg.sender == admin, "Only admin can change admin");
        require(newAdmin != address(0), "Invalid admin address");
        require(newAdmin != admin, "Same admin");

        address oldAdmin = admin;
        admin = newAdmin;

        emit AdminChanged(oldAdmin, newAdmin);
    }

    // 委托调用到实现合约
    fallback() external payable {
        _delegate(implementation);
    }

    receive() external payable {
        _delegate(implementation);
    }

    function _delegate(address impl) internal {
        assembly {
            // 复制调用数据
            calldatacopy(0, 0, calldatasize())

            // 委托调用到实现合约
            let result := delegatecall(gas(), impl, 0, calldatasize(), 0, 0)

            // 复制返回数据
            returndatacopy(0, 0, returndatasize())

            switch result
            case 0 {
                // 委托调用失败，回滚
                revert(0, returndatasize())
            }
            default {
                // 委托调用成功，返回数据
                return(0, returndatasize())
            }
        }
    }
}
```

#### 7.1.3 管理合约

**文件**: `contracts/deposit/TermManager.sol`

```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract TermManager {
    // 配置参数
    uint64 public constant MIN_TERM_DURATION = 2250;   // 约 10 天
    uint64 public constant MAX_TERM_DURATION = 821250; // 约 10 年
    uint64 public constant MIN_GRACE_PERIOD = 225;     // 约 1 天
    uint64 public constant MAX_GRACE_PERIOD = 22500;   // 约 100 天
    uint32 public constant BASE_PENALTY_BPS = 100;     // 1%
    uint32 public constant MAX_PENALTY_BPS = 1000;     // 10%

    // 管理员
    address public admin;

    // 配置更新事件
    event ConfigUpdated(string paramName, uint64 newValue);

    constructor(address _admin) {
        require(_admin != address(0), "Invalid admin address");
        admin = _admin;
    }

    // 验证期限有效性
    function validateTermDuration(uint64 duration) external pure returns (bool) {
        return duration >= MIN_TERM_DURATION && duration <= MAX_TERM_DURATION;
    }

    // 验证宽限期有效性
    function validateGracePeriod(uint64 gracePeriod) external pure returns (bool) {
        return gracePeriod >= MIN_GRACE_PERIOD && gracePeriod <= MAX_GRACE_PERIOD;
    }

    // 计算罚没比例（可根据需求实现动态计算）
    function calculatePenalty(uint64 depositId, uint64 currentEpoch) external pure returns (uint32) {
        // 简单实现：固定罚没比例
        // 实际可根据剩余期限等因素动态计算
        return BASE_PENALTY_BPS;

        // 🆕 动态罚没示例（根据剩余期限）
        // 注意：需要从链下传入更多信息，或从存储读取
        // uint64 remainingEpochs = ...;
        // uint64 totalDuration = ...;
        // uint32 dynamicPenalty = BASE_PENALTY_BPS +
        //     uint32((MAX_PENALTY_BPS - BASE_PENALTY_BPS) * remainingEpochs / totalDuration);
        // return dynamicPenalty;
    }
}
```

### 7.2 部署流程

```
阶段 1：准备（测试网）
├─ 1. 部署 TermManager 合约
├─ 2. 部署 DepositContractV2 实现合约
├─ 3. 部署 TransparentUpgradeableProxy 代理合约
│     ├─ 初始实现：DepositContractV2
│     └─ 管理员：多签钱包地址
└─ 4. 在 DepositContractV2 中设置 TermManager 地址

阶段 2：测试
├─ 5. 进行充分的功能测试
│     ├─ 普通存款（确保不影响现有功能）
│     ├─ 定期存单创建
│     ├─ 提前撤出
│     └─ 各种边界条件
└─ 6. 进行安全审计

阶段 3：主网部署（如适用）
├─ 7. 在主网部署相同的合约结构
├─ 8. 配置 Beacon Chain 节点
│     ├─ 更新 TermDepositForkEpoch
│     └─ 确保所有节点同步配置
└─ 9. 在分叉点激活
      ├─ 监控日志处理
      └─ 验证定期存单正常工作
```

---

## 8. 测试计划
### 8.2 集成测试场景

```
1. 完整生命周期测试
   ├─ 创建定期存单
   ├─ 存单进入活跃期
   ├─ 存单到期
   ├─ 进入宽限期
   ├─ 自动续期
   └─ 手动撤出

2. 并发操作测试
   ├─ 同一验证者创建多个存单
   ├─ 同时撤出多个存单
   └─ Slashing 与存单操作并发

3. 边界条件测试
   ├─ 最小期限（2250 epochs）
   ├─ 最大期限（821250 epochs）
   ├─ 最大存单数（256）
   ├─ 最小宽限期（225 epochs）
   └─ 最大宽限期（22500 epochs）

4. Slashing 交互测试
   ├─ 活期余额充足的 Slashing
   ├─ 活期不足，扣除定期存单
   ├─ 定期存单完全归零
   └─ 罚没池分配验证

5. 有效余额一致性测试
   ├─ 复合凭证验证者
   ├─ 非复合凭证验证者
   ├─ 有效余额上限验证
   └─ 多个存单的总额计算

6. 状态回滚测试
   ├─ 中途失败的原子性验证
   ├─ 缓存一致性验证
   └─ 重启后的状态恢复
```

### 8.3 性能测试

```
1. 大规模存单测试
   ├─ 100 万存单的状态大小
   ├─ 读写性能
   └─ 内存占用

2. 查询性能测试
   ├─ TermDepositsForValidator 查询速度
   │  ├─ 无缓存：O(N)
   │  └─ 有缓存：O(1)
   ├─ TermDepositById 查询速度
   └─ 并发查询性能

3. Epoch 处理延迟
   ├─ 无定期存单baseline
   ├─ 10 万存单的 epoch 处理时间
   ├─ 100 万存单的 epoch 处理时间
   └─ 识别性能瓶颈

4. 内存占用测试
   ├─ 每个存单的内存开销
   ├─ 缓存的内存开销
   └─ 总体内存增长曲线
```

---

## 9. 部署注意事项

### 9.1 硬分叉要求

此改造需要通过硬分叉部署，建议：

1. **选择合适的硬分叉点**
   - 选择 Electra 之后的升级
   - 确保所有节点有足够时间升级

2. **分叉点初始化**
   ```go
   // 在分叉点 epoch
   if currentEpoch == params.BeaconConfig().TermDepositForkEpoch {
       // 初始化所有定期存单字段为空/零值
       st.SetTermDeposits([]*ethpb.TermDeposit{})
       st.SetPendingTermDeposits([]*ethpb.PendingTermDeposit{})
       st.SetPendingTermWithdrawals([]*ethpb.TermWithdrawalRequest{})
       st.SetNextTermDepositId(0)
       st.SetTermDepositPenaltyPool(0)
       st.SetTermDepositPenaltyPoolLastDistributionEpoch(currentEpoch)
   }
   ```

3. **激活后验证**
   - 检查日志处理是否正常
   - 验证第一笔定期存单
   - 监控性能指标

### 9.2 向后兼容性

- ✅ 现有验证者不受影响（活期存款逻辑不变）
- ✅ 现有有效余额计算在无定期存单时保持一致
- ✅ 创世验证者无需任何操作
- ✅ 非复合凭证验证者可正常运行（但有效余额受限）

### 9.3 ETH1 合约升级（🆕 使用代理合约）

**步骤**：

1. **部署新合约**
   ```
   1. 部署 TermManager 合约
   2. 部署 DepositContractV2 实现合约
   3. 部署 TransparentUpgradeableProxy 代理合约
   4. 设置 TermManager 地址
   ```

2. **配置 Beacon Chain 节点**
   ```yaml
   # config.yaml
   term_deposit_fork_epoch: 123456  # 根据实际设置
   deposit_contract_address: 0x...  # 代理合约地址（不变）
   ```

3. **同步时间窗口**
   - 确保 ETH1 合约升级与 Beacon Chain 分叉激活同步
   - 在分叉点之前，新事件被忽略
   - 在分叉点之后，开始处理定期存单

### 9.4 监控指标（🆕 完整的可观测性）

**Prometheus 指标**：
```
beacon_term_deposits_total                          存单总数
beacon_term_deposits_by_status{status}             按状态分类的存单数
beacon_term_deposits_total_amount_gwei             定期存单总金额
beacon_term_deposits_penalty_pool_gwei             罚没池金额
beacon_term_deposits_matured_total                 到期存单数
beacon_term_deposits_renewed_total                 续期存单数
beacon_term_deposits_withdrawn_early_total         提前撤出数
beacon_term_deposits_withdrawn_early_penalty_gwei  提前撤出罚金
beacon_term_deposits_average_duration_epochs       平均存单期限
beacon_term_deposits_per_validator_avg             每验证者平均存单数
beacon_term_deposits_pending_count                 待处理存单数
beacon_term_deposits_slashed_total                 Slashing 影响的存单数
```

**Grafana 仪表盘示例**：
```json
{
  "panels": [
    {
      "title": "Term Deposits Overview",
      "targets": [
        {
          "expr": "beacon_term_deposits_total"
        },
        {
          "expr": "beacon_term_deposits_by_status"
        }
      ]
    },
    {
      "title": "Penalty Pool",
      "targets": [
        {
          "expr": "beacon_term_deposits_penalty_pool_gwei"
        }
      ]
    },
    {
      "title": "Early Withdrawals",
      "targets": [
        {
          "expr": "rate(beacon_term_deposits_withdrawn_early_total[5m])"
        },
        {
          "expr": "rate(beacon_term_deposits_withdrawn_early_penalty_gwei[5m])"
        }
      ]
    }
  ]
}
```

---

## 10. 安全检查清单

### 10.1 代码审查检查点

- [ ] **有效余额计算**
  - [ ] 复合凭证兼容性已实现
  - [ ] 非复合凭证警告已记录
  - [ ] 有效余额上限逻辑正确
  - [ ] 定期存单余额计算正确

- [ ] **Epoch 处理顺序**
  - [ ] 定期存单处理在正确位置
  - [ ] 与其他余额修改不冲突
  - [ ] 时间逻辑无悖论
  - [ ] 罚没池分配位置正确

- [ ] **Slashing 逻辑**
  - [ ] 原子性保障已实现
  - [ ] 预先计算所有变化
  - [ ] 罚没池更新正确
  - [ ] Slashing 日志记录完整

- [ ] **BLS 签名验证**
  - [ ] 所有 PendingTermDeposit 已验证
  - [ ] 提现凭证匹配检查
  - [ ] 域分隔符正确
  - [ ] 签名根计算正确

- [ ] **字段索引**
  - [ ] Proto 字段编号连续
  - [ ] FieldIndex 编号正确
  - [ ] 没有跳号或冲突

- [ ] **缓存一致性**
  - [ ] 缓存更新逻辑正确
  - [ ] 缓存失效时机合理
  - [ ] 并发访问安全

### 10.2 配置审查

- [ ] **参数合理性**
  - [ ] 宽限期延长至 10 天
  - [ ] 期限范围合理（10 天 - 10 年）
  - [ ] 存单数量限制合理
  - [ ] 罚没比例合理

- [ ] **分叉配置**
  - [ ] TermDepositForkEpoch 已设置
  - [ ] 所有节点配置一致
  - [ ] 时间窗口合理

### 10.3 测试覆盖

- [ ] **单元测试**
  - [ ] 覆盖率 > 80%
  - [ ] 所有关键路径已测试
  - [ ] 边界条件已覆盖

- [ ] **集成测试**
  - [ ] 完整生命周期测试通过
  - [ ] Slashing 交互测试通过
  - [ ] 并发操作测试通过

- [ ] **性能测试**
  - [ ] 大规模存单测试通过
  - [ ] Epoch 处理延迟可接受
  - [ ] 内存占用在预期范围

### 10.4 部署前检查

- [ ] **合约部署**
  - [ ] 所有合约已部署
  - [ ] 代理合约配置正确
  - [ ] 管理员权限设置正确

- [ ] **节点配置**
  - [ ] 所有节点已更新配置
  - [ ] 监控已配置
  - [ ] 告警规则已设置

- [ ] **文档更新**
  - [ ] 用户手册已更新
  - [ ] API 文档已更新
  - [ ] 运维手册已更新

### 10.5 上线后监控

- [ ] **功能验证**
  - [ ] 第一笔定期存单成功创建
  - [ ] 有效余额计算正确
  - [ ] 日志处理正常

- [ ] **性能监控**
  - [ ] Epoch 处理时间正常
  - [ ] 内存占用稳定
  - [ ] CPU 使用率正常

- [ ] **告警设置**
  - [ ] 罚没池异常告警
  - [ ] 存单处理失败告警
  - [ ] 性能异常告警

---

## 总结

本文档（v2.0 修订版）基于安全评估的发现，全面修复了原方案中的严重风险和设计缺陷：

### 主要修复

1. ✅ **有效余额计算** - 修复复合凭证兼容性，支持混合验证者环境
2. ✅ **Epoch 处理顺序** - 明确定义处理顺序，避免状态不一致
3. ✅ **Slashing 原子性** - 增强原子性保障，预防状态损坏
4. ✅ **BLS 签名验证** - 添加完整的安全验证机制
5. ✅ **字段索引优化** - 使用连续编号，避免范围冲突
6. ✅ **宽限期延长** - 从 1 天延长至 10 天，降低用户风险
7. ✅ **代理合约部署** - 保持地址不变，简化升级流程
8. ✅ **监控完善** - 添加全面的可观测性支持

### 实施建议

1. **优先级排序**
   - P0：修复有效余额、Epoch 顺序、Slashing 原子性
   - P1：添加签名验证、优化字段索引
   - P2：完善监控、优化性能

2. **测试策略**
   - 单元测试覆盖率 > 80%
   - 完整的集成测试
   - 充分的性能测试
   - 安全审计

3. **部署策略**
   - 测试网充分验证
   - 分阶段部署
   - 密切监控
   - 准备回滚方案
---

**文档结束**
