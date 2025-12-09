# Prysm 验证者定期存单改造实现指南（修正版 v3.5）

## 版本信息

- 文档版本：v3.5（修正版）
- 修订日期：2025-12-09
- 基于 Prysm 版本：v6.x (Electra fork)

### v3.5 更新内容

- 完善附录 A 编译和测试章节，新增详细的编译和 Docker 镜像构建命令
- 新增 A.2 编译项目章节：包含 Bazel 和 Go 两种编译方式
- 新增 A.3 构建 Docker 镜像章节：包含本地 tarball、远程推送、portable 版本说明
- 新增 A.4 运行测试章节：补充 Bazel 测试命令

### v3.4 更新内容

- 新增第 22 节：rootSelector() 函数修改（关键）
- 新增第 23 节：beaconStateMarshalable 结构体修改
- 新增第 24 节：minimal_config.go Term Deposit 配置
- 新增第 25 节：BUILD.bazel 文件修改清单（关键）
- 更新附录 A 检查清单，增加 BUILD.bazel 修改项

### v3.3 更新内容

- 新增第 21 节：State Proto 转换函数修改（getters_state.go）
- 补充 `config/fieldparams/minimal.go` 的 Limit 常量修改说明
- 更新附录 A 检查清单，增加 `getters_state.go` 修改项

### v3.2 更新内容

- 新增第 17 节：FieldCount 常量更新（关键修改）
- 新增第 18 节：Hash Tree Root 计算修改（hasher.go）
- 新增第 19 节：StateUtil Root 函数（3 个新文件）
- 新增第 20 节：FieldParams 限制常量
- 新增附录 A：完整修改检查清单

### v3.1 更新内容

- 修正第 14 节 `state_trie.go` 的修改指导，正确描述 Prysm 的 Copy-on-Write 机制
- 添加 `electraFields` 数组、`sharedFieldRefCount` 常量的修改说明
- 添加 `InitializeFromProtoUnsafeElectra/Fulu` 函数的详细修改步骤
- 说明共享引用计数的自动处理机制

---

## 目录

1. [项目概述](#1-项目概述)
2. [文件修改清单](#2-文件修改清单)
3. [Proto 定义](#3-proto-定义)
4. [配置参数](#4-配置参数)
5. [State 类型定义](#5-state-类型定义)
6. [State 接口定义](#6-state-接口定义)
7. [State Getter 实现](#7-state-getter-实现)
8. [State Setter 实现](#8-state-setter-实现)
9. [核心逻辑实现](#9-核心逻辑实现)
10. [Epoch 处理修改](#10-epoch-处理修改)
11. [有效余额计算修改](#11-有效余额计算修改)
12. [Slashing 逻辑修改](#12-slashing-逻辑修改)
13. [状态升级函数修改](#13-状态升级函数修改)
14. [State Trie 修改](#14-state-trie-修改)
15. [Genesis State 初始化修改](#15-genesis-state-初始化修改)
16. [测试工具修改](#16-测试工具修改)
17. [FieldCount 常量更新（关键）](#17-fieldcount-常量更新关键)
18. [Hash Tree Root 计算修改（关键）](#18-hash-tree-root-计算修改关键)
19. [StateUtil Root 函数（关键）](#19-stateutil-root-函数关键)
20. [FieldParams 限制常量（关键）](#20-fieldparams-限制常量关键)
21. [State Proto 转换函数修改（关键）](#21-state-proto-转换函数修改)
22. [rootSelector() 函数修改（关键）](#22-rootselector-函数修改关键)
23. [beaconStateMarshalable 结构体修改](#23-beaconstatemarshalable-结构体修改)
24. [minimal_config.go Term Deposit 配置](#24-minimal_configgo-term-deposit-配置)
25. [BUILD.bazel 文件修改清单（关键）](#25-buildbazel-文件修改清单关键)

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

---

## 2. 文件修改清单

### 2.1 新建文件

| 文件路径 | 描述 |
|---------|------|
| `proto/prysm/v1alpha1/term_deposit.proto` | 定期存单 protobuf 定义 |
| `beacon-chain/core/electra/term_deposit.go` | 存单核心处理逻辑（Epoch 处理相关） |
| `beacon-chain/core/helpers/term_deposit.go` | 存单辅助函数（SlashTermDeposits，避免循环引用） |
| `beacon-chain/core/validators/log.go` | validators 包日志定义（原包无日志支持） |
| `beacon-chain/state/state-native/getters_term_deposit.go` | State getter 方法 |
| `beacon-chain/state/state-native/setters_term_deposit.go` | State setter 方法 |
| `beacon-chain/state/stateutil/term_deposits_root.go` | **TermDeposits SSZ root 计算** |
| `beacon-chain/state/stateutil/pending_term_deposits_root.go` | **PendingTermDeposits SSZ root 计算** |
| `beacon-chain/state/stateutil/pending_term_withdrawals_root.go` | **PendingTermWithdrawals SSZ root 计算** |

### 2.2 修改文件

| 文件路径 | 修改类型 |
|---------|---------|
| `proto/prysm/v1alpha1/beacon_state.proto` | 新增字段 |
| `proto/prysm/v1alpha1/BUILD.bazel` | SSZ 配置 |
| `config/params/config.go` | 新增配置参数 |
| `config/params/mainnet_config.go` | 新增配置值 + **FieldCount 更新** |
| `config/fieldparams/mainnet.go` | **新增 Limit 常量** |
| `config/fieldparams/minimal.go` | 新增 Limit 常量（可选） |
| `beacon-chain/state/state-native/types/types.go` | 新增 FieldIndex |
| `beacon-chain/state/interfaces.go` | 新增接口方法 |
| `beacon-chain/state/state-native/beacon_state.go` | 新增字段 |
| `beacon-chain/state/state-native/state_trie.go` | Copy() 函数添加字段复制 |
| `beacon-chain/state/state-native/hasher.go` | **Hash Tree Root 计算** |
| `beacon-chain/core/electra/transition.go` | 调用存单处理 |
| `beacon-chain/core/electra/effective_balance_updates.go` | 有效余额计算 |
| `beacon-chain/core/electra/upgrade.go` | 状态升级初始化 term deposit |
| `beacon-chain/core/fulu/upgrade.go` | 状态升级继承 term deposit |
| `beacon-chain/core/validators/validator.go` | Slashing 处理 |
| `runtime/interop/premine-state.go` | Genesis 状态初始化 |
| `testing/util/electra_state.go` | 测试工具初始化 |

---

## 3. Proto 定义

### 3.1 新建文件：`proto/prysm/v1alpha1/term_deposit.proto`

```protobuf
// Copyright 2024 Prysmatic Labs.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
syntax = "proto3";

package ethereum.eth.v1alpha1;

import "proto/eth/ext/options.proto";

option csharp_namespace = "Ethereum.Eth.v1alpha1";
option go_package = "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1;eth";
option java_multiple_files = true;
option java_outer_classname = "TermDepositProto";
option java_package = "org.ethereum.eth.v1alpha1";
option php_namespace = "Ethereum\\Eth\\v1alpha1";

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
```

### 3.2 修改文件：`proto/prysm/v1alpha1/beacon_state.proto`

在 `BeaconStateElectra` 消息末尾添加（12009 之后）：

```protobuf
// 在文件顶部添加 import
import "proto/prysm/v1alpha1/term_deposit.proto";

// 在 BeaconStateElectra 消息中，12009 之后添加：

  // ========== 定期存单相关字段 [12010-12016] ==========

  // 所有定期存单列表
  repeated TermDeposit term_deposits = 12010
      [(ethereum.eth.ext.ssz_max) = "1048576"];

  // 待处理的定期存单队列
  repeated PendingTermDeposit pending_term_deposits = 12011
      [(ethereum.eth.ext.ssz_max) = "65536"];

  // 下一个存单 ID（自增）
  uint64 next_term_deposit_id = 12012;

  // 待处理的存单撤出请求
  repeated TermWithdrawalRequest pending_term_withdrawals = 12013
      [(ethereum.eth.ext.ssz_max) = "65536"];

  // 公共奖励池（存放罚没金额）
  uint64 term_deposit_penalty_pool = 12014;

  // 罚没池上次分配的 epoch
  uint64 term_deposit_penalty_pool_last_distribution_epoch = 12015
      [(ethereum.eth.ext.cast_type) =
       "github.com/OffchainLabs/prysm/v6/consensus-types/primitives.Epoch"];
```

**注意**：同样需要在 `BeaconStateFulu` 中添加相同的字段。

### 3.3 修改文件：`proto/prysm/v1alpha1/BUILD.bazel`

需要修改两个地方：

> **重要说明**：`term_deposit.proto` 只需要添加到 `ssz_proto_files` 中，**不要**同时添加到 `proto_library` 的 `srcs` 中，否则会导致 Bazel 编译时出现路径冲突错误：
> ```
> proto files ... have the same import path
> ```

#### 3.3.1 在 `ssz_electra_objs` 列表中添加新类型（用于 SSZ 序列化）

```starlark
ssz_electra_objs = [
    "AggregateAttestationAndProofElectra",
    "AggregateAttestationAndProofSingle",
    "AttestationElectra",
    "AttesterSlashingElectra",
    "BeaconBlockElectra",
    "BeaconBlockBodyElectra",
    "BeaconBlockContentsElectra",
    "BeaconStateElectra",
    "BlindedBeaconBlockBodyElectra",
    "BlindedBeaconBlockElectra",
    "BuilderBidElectra",
    "Consolidation",
    "IndexedAttestationElectra",
    "LightClientHeaderElectra",
    "LightClientBootstrapElectra",
    "LightClientUpdateElectra",
    "LightClientFinalityUpdateElectra",
    "PendingDeposit",
    "PendingDeposits",
    "PendingConsolidation",
    "PendingPartialWithdrawal",
    "PendingTermDeposit",       # 新增
    "SignedAggregateAttestationAndProofElectra",
    "SignedAggregateAttestationAndProofSingle",
    "SignedBeaconBlockContentsElectra",
    "SignedBeaconBlockElectra",
    "SignedBlindedBeaconBlockElectra",
    "SignedConsolidation",
    "SingleAttestation",
    "SignedBuilderBidElectra",
    "TermDeposit",              # 新增
    "TermWithdrawalRequest",    # 新增
]
```

#### 3.3.3 在 `ssz_proto_files` 的 `srcs` 列表中添加 proto 文件：

```starlark
ssz_proto_files(
    name = "ssz_proto_files",
    srcs = [
        "attestation.proto",
        "beacon_block.proto",
        "beacon_core_types.proto",
        "beacon_state.proto",
        "blobs.proto",
        "data_columns.proto",
        "light_client.proto",
        "sync_committee.proto",
        "term_deposit.proto",  # 新增
        "withdrawals.proto",
    ],
    config = select({
        "//conditions:default": "mainnet",
        "//proto:ssz_mainnet": "mainnet",
        "//proto:ssz_minimal": "minimal",
    }),
)
```

### 3.4 编译 Proto 文件

**重要**：在修改完所有 proto 文件后，必须执行以下命令生成 Go 代码：

```bash
# 在项目根目录执行
./hack/update-go-pbs.sh
```

该脚本会：

1. 使用 Bazel 编译 `//proto/...` 目录下的所有 proto 文件
2. 将生成的 `.pb.go` 文件从 `bazel-bin/` 复制回 `proto/` 目录
3. 运行 `goimports` 和 `gofmt` 格式化生成的代码

编译成功后，以下类型将可用：

- `ethpb.TermDeposit`
- `ethpb.PendingTermDeposit`
- `ethpb.TermWithdrawalRequest`

**注意**：只有在 proto 编译完成后，才能修改 `beacon_state.go` 等 Go 文件引用这些类型。

---

## 4. 配置参数

### 4.1 修改文件：`config/params/config.go`

在 `BeaconChainConfig` 结构体中添加：

```go
// 在 BeaconChainConfig 结构体中添加以下字段
// （建议放在 Electra 相关配置附近）

// ========== 定期存单配置 ==========

// TermDepositsLimit 最大存单数量
TermDepositsLimit uint64 `yaml:"TERM_DEPOSITS_LIMIT" spec:"true"`

// PendingTermDepositsLimit 最大待处理存单数量
PendingTermDepositsLimit uint64 `yaml:"PENDING_TERM_DEPOSITS_LIMIT" spec:"true"`

// MaxTermDepositsPerValidator 每个验证者最大存单数
MaxTermDepositsPerValidator uint64 `yaml:"MAX_TERM_DEPOSITS_PER_VALIDATOR" spec:"true"`

// DefaultTermDepositGracePeriod 默认宽限期（epoch 数）
DefaultTermDepositGracePeriod uint64 `yaml:"DEFAULT_TERM_DEPOSIT_GRACE_PERIOD" spec:"true"`

// MinTermDepositGracePeriod 最小宽限期（epoch 数）
MinTermDepositGracePeriod uint64 `yaml:"MIN_TERM_DEPOSIT_GRACE_PERIOD" spec:"true"`

// MaxTermDepositGracePeriod 最大宽限期（epoch 数）
MaxTermDepositGracePeriod uint64 `yaml:"MAX_TERM_DEPOSIT_GRACE_PERIOD" spec:"true"`

// MinTermDepositDuration 最小期限（epoch 数）
MinTermDepositDuration uint64 `yaml:"MIN_TERM_DEPOSIT_DURATION" spec:"true"`

// MaxTermDepositDuration 最大期限（epoch 数）
MaxTermDepositDuration uint64 `yaml:"MAX_TERM_DEPOSIT_DURATION" spec:"true"`

// EarlyWithdrawalPenaltyBaseBps 提前撤出基础罚没比例（basis points）
EarlyWithdrawalPenaltyBaseBps uint64 `yaml:"EARLY_WITHDRAWAL_PENALTY_BASE_BPS" spec:"true"`

// MaxTermDepositRenewals 最大续期次数（0 表示无限制）
MaxTermDepositRenewals uint64 `yaml:"MAX_TERM_DEPOSIT_RENEWALS" spec:"true"`

// TermDepositPenaltyPoolDistributionInterval 罚没池分配间隔（epoch 数）
TermDepositPenaltyPoolDistributionInterval uint64 `yaml:"TERM_DEPOSIT_PENALTY_POOL_DISTRIBUTION_INTERVAL" spec:"true"`

// TermDepositForkEpoch 定期存单分叉激活 epoch
TermDepositForkEpoch primitives.Epoch `yaml:"TERM_DEPOSIT_FORK_EPOCH" spec:"true"`
```

### 4.2 修改文件：`config/params/mainnet_config.go`

在 `mainnetBeaconConfig` 变量中添加配置值：

```go
// 在 mainnetBeaconConfig 中添加

// 定期存单配置
TermDepositsLimit:                          1 << 20, // 1,048,576
PendingTermDepositsLimit:                   1 << 16, // 65,536
MaxTermDepositsPerValidator:                256,
DefaultTermDepositGracePeriod:              4050,            // 约 3 天 (假设 12 秒/slot)
MinTermDepositGracePeriod:                  1350,            // 约 1 天
MaxTermDepositGracePeriod:                  40500,           // 约 30 天
MinTermDepositDuration:                     4050,            // 约 3 天
MaxTermDepositDuration:                     1350 * 365 * 10, // 约 10 年
EarlyWithdrawalPenaltyBaseBps:              100,             // 1%
MaxTermDepositRenewals:                     0,               // 0 = 无限制
TermDepositPenaltyPoolDistributionInterval: 1350,            // 每天分配一次
TermDepositForkEpoch:                       0,               // 创世即启用（私链）
```

---

## 5. State 类型定义

### 5.1 修改文件：`beacon-chain/state/state-native/types/types.go`

在 `FieldIndex` 常量定义中添加：

```go
const (
    // ... 现有定义 ...
    ProposerLookahead             // Fulu: EIP-7917

    // 定期存单相关字段（在 ProposerLookahead 之后添加）
    TermDeposits                         // 所有定期存单列表
    PendingTermDeposits                  // 待处理的定期存单队列
    NextTermDepositId                    // 下一个存单 ID
    PendingTermWithdrawals               // 待处理的存单撤出请求
    TermDepositPenaltyPool               // 公共奖励池
    TermDepositPenaltyPoolLastDistEpoch  // 罚没池上次分配的 epoch
)
```

在 `String()` 方法中添加：

```go
func (f FieldIndex) String() string {
    switch f {
    // ... 现有 case ...
    case TermDeposits:
        return "termDeposits"
    case PendingTermDeposits:
        return "pendingTermDeposits"
    case NextTermDepositId:
        return "nextTermDepositId"
    case PendingTermWithdrawals:
        return "pendingTermWithdrawals"
    case TermDepositPenaltyPool:
        return "termDepositPenaltyPool"
    case TermDepositPenaltyPoolLastDistEpoch:
        return "termDepositPenaltyPoolLastDistEpoch"
    default:
        return fmt.Sprintf("unknown field index number: %d", f)
    }
}
```

在 `RealPosition()` 方法中添加（继续现有编号）：

```go
func (f FieldIndex) RealPosition() int {
    switch f {
    // ... 现有 case ...
    case ProposerLookahead:
        return 37
    case TermDeposits:
        return 38
    case PendingTermDeposits:
        return 39
    case NextTermDepositId:
        return 40
    case PendingTermWithdrawals:
        return 41
    case TermDepositPenaltyPool:
        return 42
    case TermDepositPenaltyPoolLastDistEpoch:
        return 43
    default:
        return -1
    }
}
```

### 5.2 修改文件：`beacon-chain/state/state-native/beacon_state.go`

在 `BeaconState` 结构体中添加字段：

```go
type BeaconState struct {
    // ... 现有字段 ...

    // Fulu 字段之后添加定期存单字段
    proposerLookahead []primitives.ValidatorIndex

    // 定期存单字段
    termDeposits                          []*ethpb.TermDeposit
    pendingTermDeposits                   []*ethpb.PendingTermDeposit
    nextTermDepositId                     uint64
    pendingTermWithdrawals                []*ethpb.TermWithdrawalRequest
    termDepositPenaltyPool                uint64
    termDepositPenaltyPoolLastDistEpoch   primitives.Epoch

    // ... 其他现有字段 ...
}
```

---

## 6. State 接口定义

### 6.1 修改文件：`beacon-chain/state/interfaces.go`

添加定期存单相关接口：

```go
// ReadOnlyTermDeposits 定义只读的定期存单接口
type ReadOnlyTermDeposits interface {
    TermDeposits() ([]*ethpb.TermDeposit, error)
    TermDepositsForValidator(idx primitives.ValidatorIndex) ([]*ethpb.TermDeposit, error)
    TermDepositById(depositId uint64) (*ethpb.TermDeposit, uint64, error)
    PendingTermDeposits() ([]*ethpb.PendingTermDeposit, error)
    PendingTermWithdrawals() ([]*ethpb.TermWithdrawalRequest, error)
    NextTermDepositId() (uint64, error)
    TermDepositPenaltyPool() (uint64, error)
    TermDepositPenaltyPoolLastDistributionEpoch() (primitives.Epoch, error)
}

// WriteOnlyTermDeposits 定义只写的定期存单接口
type WriteOnlyTermDeposits interface {
    AppendTermDeposit(td *ethpb.TermDeposit) error
    UpdateTermDepositAtIndex(idx uint64, td *ethpb.TermDeposit) error
    UpdateTermDepositById(depositId uint64, td *ethpb.TermDeposit) error
    SetTermDeposits(tds []*ethpb.TermDeposit) error
    SetPendingTermDeposits(ptds []*ethpb.PendingTermDeposit) error
    AppendPendingTermDeposit(ptd *ethpb.PendingTermDeposit) error
    SetPendingTermWithdrawals(reqs []*ethpb.TermWithdrawalRequest) error
    SetNextTermDepositId(id uint64) error
    SetTermDepositPenaltyPool(amount uint64) error
    SetTermDepositPenaltyPoolLastDistributionEpoch(epoch primitives.Epoch) error
}
```

修改 `ReadOnlyBeaconState` 接口，添加：

```go
type ReadOnlyBeaconState interface {
    // ... 现有接口 ...
    ReadOnlyTermDeposits
}
```

修改 `WriteOnlyBeaconState` 接口，添加：

```go
type WriteOnlyBeaconState interface {
    // ... 现有接口 ...
    WriteOnlyTermDeposits
}
```

---

## 7. State Getter 实现

### 7.1 新建文件：`beacon-chain/state/state-native/getters_term_deposit.go`

```go
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
            copy := &ethpb.TermDeposit{
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
            result = append(result, copy)
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
            copy := &ethpb.TermDeposit{
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
            return copy, uint64(i), nil
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
```

---

## 8. State Setter 实现

### 8.1 新建文件：`beacon-chain/state/state-native/setters_term_deposit.go`

```go
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
```

---

## 9. 核心逻辑实现

### 9.1 新建文件：`beacon-chain/core/electra/term_deposit.go`

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
                    "deposit_id":     td.DepositId,
                    "validator":      td.ValidatorIndex,
                    "grace_end":      graceEndEpoch,
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
                "validator":             validatorIndex,
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

```

> **注意**：`SlashTermDeposits` 函数已移至 `helpers` 包中（见 9.2 节），以避免与 `validators` 包的循环引用问题。

---

## 9.2 新建文件：`beacon-chain/core/helpers/term_deposit.go`

> **重要**：此函数放在 `helpers` 包中，而不是 `electra` 包中，是为了避免循环引用：
> - `validators/validator.go` 需要调用 `SlashTermDeposits`
> - 如果放在 `electra` 包中会导致：`validators` → `electra` → `validators` 循环依赖

```go
package helpers

import (
    "github.com/OffchainLabs/prysm/v6/beacon-chain/state"
    "github.com/OffchainLabs/prysm/v6/consensus-types/primitives"
    "github.com/pkg/errors"
    log "github.com/sirupsen/logrus"
)

// 定期存单状态常量
const (
    TermDepositStatusActive            = 0 // 活跃（未到期）
    TermDepositStatusMatured           = 1 // 已到期（宽限期内）
    TermDepositStatusWithdrawn         = 2 // 已撤出
    TermDepositStatusPendingWithdrawal = 3 // 撤出中
)

// SlashTermDeposits 处理 Slashing 时的定期存单扣除
// 返回实际从定期存单扣除的金额
//
// 注意：此函数放在 helpers 包中而非 electra 包中，是为了避免
// validators 包与 electra 包之间的循环引用。
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
```

---

## 10. Epoch 处理修改

### 10.1 修改文件：`beacon-chain/core/electra/transition.go`

修改 `ProcessEpoch` 函数，在适当位置添加定期存单处理：

```go
// ProcessEpoch 描述每个 epoch 执行的操作
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

    // ========== 定期存单处理 (在 ProcessPendingConsolidations 之后，ProcessEffectiveBalanceUpdates 之前) ==========
    if err = ProcessTermDepositMaturity(ctx, state); err != nil {
        return errors.Wrap(err, "could not process term deposit maturity")
    }
    if err = ProcessPendingTermDeposits(ctx, state); err != nil {
        return errors.Wrap(err, "could not process pending term deposits")
    }
    if err = ProcessTermWithdrawalRequests(ctx, state); err != nil {
        return errors.Wrap(err, "could not process term withdrawal requests")
    }
    // ========== 定期存单处理结束 ==========

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

    // ========== 罚没池分配 (在所有处理完成后) ==========
    if err = ProcessTermDepositPenaltyDistribution(ctx, state); err != nil {
        return errors.Wrap(err, "could not distribute penalty pool")
    }
    // ========== 罚没池分配结束 ==========

    return nil
}
```

---

## 11. 有效余额计算修改

### 11.1 修改文件：`beacon-chain/core/electra/effective_balance_updates.go`

```go
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
//  def process_effective_balance_updates(state: BeaconState) -> None:
//      # Update effective balances with hysteresis
//      for index, validator in enumerate(state.validators):
//          balance = state.balances[index]
//          HYSTERESIS_INCREMENT = uint64(EFFECTIVE_BALANCE_INCREMENT // HYSTERESIS_QUOTIENT)
//          DOWNWARD_THRESHOLD = HYSTERESIS_INCREMENT * HYSTERESIS_DOWNWARD_MULTIPLIER
//          UPWARD_THRESHOLD = HYSTERESIS_INCREMENT * HYSTERESIS_UPWARD_MULTIPLIER
//          EFFECTIVE_BALANCE_LIMIT = (
//              MAX_EFFECTIVE_BALANCE_EIP7251 if has_compounding_withdrawal_credential(validator)
//              else MIN_ACTIVATION_BALANCE
//          )
//
//          if (
//              balance + DOWNWARD_THRESHOLD < validator.effective_balance
//              or validator.effective_balance + UPWARD_THRESHOLD < balance
//          ):
//              validator.effective_balance = min(balance - balance % EFFECTIVE_BALANCE_INCREMENT, EFFECTIVE_BALANCE_LIMIT)
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
```

---

## 12. Slashing 逻辑修改

### 12.1 新建文件：`beacon-chain/core/validators/log.go`

> **重要**：`validators` 包中原本没有日志支持，需要先创建 `log.go` 文件。

```go
package validators

import "github.com/sirupsen/logrus"

var log = logrus.WithField("prefix", "validators")
```

### 12.2 修改文件：`beacon-chain/core/validators/validator.go`

在 `SlashValidator` 函数中添加定期存单处理：

> **注意**：这里调用的是 `helpers.SlashTermDeposits`（在 `helpers/term_deposit.go` 中定义），
> 而不是 `electra` 包中的函数，以避免循环引用问题。

```go
// 在文件顶部添加导入
import (
    // ... 现有导入 ...
    "github.com/OffchainLabs/prysm/v6/beacon-chain/core/helpers"  // 通常已导入
    "github.com/sirupsen/logrus"  // 用于 logrus.Fields
)

// SlashValidator slashes the malicious validator's balance
func SlashValidator(
    ctx context.Context,
    s state.BeaconState,
    slashedIdx primitives.ValidatorIndex,
    exitInfo *ExitInfo,
) (state.BeaconState, error) {
    var err error
    if exitInfo == nil {
        return nil, errors.New("exit info is required to slash validator")
    }
    s, err = InitiateValidatorExitForTotalBal(ctx, s, slashedIdx, exitInfo, primitives.Gwei(exitInfo.TotalActiveBalance))
    if err != nil && !errors.Is(err, ErrValidatorAlreadyExited) {
        return nil, errors.Wrapf(err, "could not initiate validator %d exit", slashedIdx)
    }
    currentEpoch := slots.ToEpoch(s.Slot())
    validator, err := s.ValidatorAtIndex(slashedIdx)
    if err != nil {
        return nil, err
    }
    validator.Slashed = true
    maxWithdrawableEpoch := primitives.MaxEpoch(validator.WithdrawableEpoch, currentEpoch+params.BeaconConfig().EpochsPerSlashingsVector)
    validator.WithdrawableEpoch = maxWithdrawableEpoch

    if err := s.UpdateValidatorAtIndex(slashedIdx, validator); err != nil {
        return nil, err
    }

    // The slashing amount is represented by epochs per slashing vector.
    slashings := s.Slashings()
    currentSlashing := slashings[currentEpoch%params.BeaconConfig().EpochsPerSlashingsVector]
    if err := s.UpdateSlashingsAtIndex(
        uint64(currentEpoch%params.BeaconConfig().EpochsPerSlashingsVector),
        currentSlashing+validator.EffectiveBalance,
    ); err != nil {
        return nil, err
    }

    slashingQuotient, proposerRewardQuotient, whistleblowerRewardQuotient, err := SlashingParamsPerVersion(s.Version())
    if err != nil {
        return nil, errors.Wrap(err, "could not get slashing parameters per version")
    }

    slashingPenalty, err := math.Div64(validator.EffectiveBalance, slashingQuotient)
    if err != nil {
        return nil, errors.Wrap(err, "failed to compute slashing penalty")
    }

    // ========== 修改：先从活期扣除，不足时从定期存单扣除 ==========
    demandBalance, err := s.BalanceAtIndex(slashedIdx)
    if err != nil {
        return nil, err
    }

    if demandBalance >= slashingPenalty {
        // 活期余额充足，直接扣除
        if err := helpers.DecreaseBalance(s, slashedIdx, slashingPenalty); err != nil {
            return nil, err
        }
    } else {
        // 活期不足，扣除全部活期后从定期存单扣除
        if err := helpers.DecreaseBalance(s, slashedIdx, demandBalance); err != nil {
            return nil, err
        }

        remainingPenalty := slashingPenalty - demandBalance

        // 注意：这里使用 logrus.Fields 而不是 log.Fields
        // 因为 log 是通过 log.go 定义的 *logrus.Entry 变量，不是 logrus 包别名
        log.WithFields(logrus.Fields{
            "validator":         slashedIdx,
            "slashing_penalty":  slashingPenalty,
            "demand_balance":    demandBalance,
            "remaining_penalty": remainingPenalty,
        }).Info("Demand balance insufficient for slashing penalty, deducting from term deposits")

        // 从定期存单中扣除剩余惩罚（调用 helpers 包中的函数，避免循环引用）
        _, err := helpers.SlashTermDeposits(s, slashedIdx, remainingPenalty)
        if err != nil {
            return nil, errors.Wrap(err, "failed to slash term deposits")
        }
    }
    // ========== 修改结束 ==========

    proposerIdx, err := helpers.BeaconProposerIndex(ctx, s)
    if err != nil {
        return nil, errors.Wrap(err, "could not get proposer idx")
    }
    whistleBlowerIdx := proposerIdx
    whistleblowerReward, err := math.Div64(validator.EffectiveBalance, whistleblowerRewardQuotient)
    if err != nil {
        return nil, errors.Wrap(err, "failed to compute whistleblowerReward")
    }
    proposerReward, err := math.Div64(whistleblowerReward, proposerRewardQuotient)
    if err != nil {
        return nil, errors.Wrap(err, "failed to compute proposer reward")
    }
    if err := helpers.IncreaseBalance(s, proposerIdx, proposerReward); err != nil {
        return nil, err
    }
    if err := helpers.IncreaseBalance(s, whistleBlowerIdx, whistleblowerReward-proposerReward); err != nil {
        return nil, err
    }
    return s, nil
}
```

---

## 附录 A：编译和测试

### A.1 生成 Proto 代码

```bash
# 在项目根目录执行
make proto
```

### A.2 编译项目

#### A.2.1 使用 Bazel 编译（推荐）

```bash
# 编译所有目标
bazel build //...

# 分别编译三个主要应用
bazel build //cmd/beacon-chain:beacon-chain    # 信标链节点
bazel build //cmd/validator:validator          # 验证者客户端
bazel build //cmd/prysmctl:prysmctl            # 管理工具

# Release 模式编译（优化后的生产环境二进制）
bazel build --config=release //cmd/beacon-chain:beacon-chain
bazel build --config=release //cmd/validator:validator
bazel build --config=release //cmd/prysmctl:prysmctl
```

编译产物位置：

- `bazel-bin/cmd/beacon-chain/beacon-chain_/beacon-chain`
- `bazel-bin/cmd/validator/validator_/validator`
- `bazel-bin/cmd/prysmctl/prysmctl_/prysmctl`

### A.3 构建 Docker 镜像

Prysm 使用 Bazel + rules_oci 构建 Docker 镜像，镜像支持多架构（amd64 + arm64）。

#### A.3.1 构建本地 tarball 镜像（用于本地测试）

```bash
# 构建 beacon-chain Docker tarball
bazel build --config=release //cmd/beacon-chain:oci_image_tarball
docker load < bazel-bin/cmd/beacon-chain/oci_image_tarball/tarball.tar

# 构建 validator Docker tarball
bazel build --config=release //cmd/validator:oci_image_tarball
docker load < bazel-bin/cmd/validator/oci_image_tarball/tarball.tar

# 构建 prysmctl Docker tarball
bazel build --config=release //cmd/prysmctl:oci_image_tarball
docker load < bazel-bin/cmd/prysmctl/oci_image_tarball/tarball.tar
```

#### A.3.2 推送镜像到远程仓库

```bash
# 使用提供的脚本一次性构建并推送所有镜像
./hack/build_and_upload_docker.sh <tag>

# 或分别推送各个镜像
bazel run --config=release //cmd/beacon-chain:push_images -- --tag=<your-tag>
bazel run --config=release //cmd/validator:push_images -- --tag=<your-tag>
bazel run --config=release //cmd/prysmctl:push_images -- --tag=<your-tag>
```

#### A.3.3 构建 Portable 版本（兼容旧 CPU）

如果需要在不支持现代 CPU 指令集的机器上运行，使用 portable 模式：

```bash
# 构建 portable 版本的 beacon-chain
bazel build --config=release --define=blst_modern=false //cmd/beacon-chain:oci_image_tarball
```

#### A.3.4 镜像 Target 汇总表

| 组件 | Tarball Target | Push Target |
|------|----------------|-------------|
| beacon-chain | `//cmd/beacon-chain:oci_image_tarball` | `//cmd/beacon-chain:push_images` |
| validator | `//cmd/validator:oci_image_tarball` | `//cmd/validator:push_images` |
| prysmctl | `//cmd/prysmctl:oci_image_tarball` | `//cmd/prysmctl:push_images` |

### A.4 运行测试

```bash
# 运行定期存单相关测试
go test ./beacon-chain/core/electra/... -v -run Term
go test ./beacon-chain/state/state-native/... -v -run Term

# 使用 Bazel 运行测试
bazel test //beacon-chain/core/electra:go_default_test
bazel test //beacon-chain/state/state-native:go_default_test
```

---

## 13. 状态升级函数修改

> **重要**：当链从 Deneb 升级到 Electra 或从 Electra 升级到 Fulu 时，需要正确初始化 term deposit 字段。

### 13.1 修改文件：`beacon-chain/core/electra/upgrade.go`

在 `ConvertToElectra()` 函数中的 `ethpb.BeaconStateElectra{}` 初始化部分添加 term deposit 字段：

```go
// 在 s := &ethpb.BeaconStateElectra{...} 中添加以下字段（约 L131 之后）

    PendingConsolidations:      make([]*ethpb.PendingConsolidation, 0),

    // ========== 新增：定期存单字段初始化 ==========
    TermDeposits:                              make([]*ethpb.TermDeposit, 0),
    PendingTermDeposits:                       make([]*ethpb.PendingTermDeposit, 0),
    NextTermDepositId:                         0,
    PendingTermWithdrawals:                    make([]*ethpb.TermWithdrawalRequest, 0),
    TermDepositPenaltyPool:                    0,
    TermDepositPenaltyPoolLastDistributionEpoch: 0,
    // ========== 新增结束 ==========
}
```

### 13.2 修改文件：`beacon-chain/core/fulu/upgrade.go`

在 `ConvertToFulu()` 函数中，从 Electra 状态继承 term deposit 字段：

```go
// 在创建 BeaconStateFulu 时，需要继承 Electra 的 term deposit 字段

// 获取 Electra 的 term deposit 数据
termDeposits, err := beaconState.TermDeposits()
if err != nil {
    return nil, err
}
pendingTermDeposits, err := beaconState.PendingTermDeposits()
if err != nil {
    return nil, err
}
nextTermDepositId, err := beaconState.NextTermDepositId()
if err != nil {
    return nil, err
}
pendingTermWithdrawals, err := beaconState.PendingTermWithdrawals()
if err != nil {
    return nil, err
}
termDepositPenaltyPool, err := beaconState.TermDepositPenaltyPool()
if err != nil {
    return nil, err
}
termDepositPenaltyPoolLastDistEpoch, err := beaconState.TermDepositPenaltyPoolLastDistributionEpoch()
if err != nil {
    return nil, err
}

// 在 BeaconStateFulu 初始化中添加：
s := &ethpb.BeaconStateFulu{
    // ... 其他字段 ...

    // 继承 term deposit 字段
    TermDeposits:                              termDeposits,
    PendingTermDeposits:                       pendingTermDeposits,
    NextTermDepositId:                         nextTermDepositId,
    PendingTermWithdrawals:                    pendingTermWithdrawals,
    TermDepositPenaltyPool:                    termDepositPenaltyPool,
    // 注意：不需要类型转换，getter 返回的已经是 primitives.Epoch 类型
    TermDepositPenaltyPoolLastDistributionEpoch: termDepositPenaltyPoolLastDistEpoch,
}
```

---

## 14. State Trie 修改

> **重要**：`state_trie.go` 需要修改多个位置来支持 term deposit 字段。Prysm 使用 Copy-on-Write 优化，
> 切片类型字段通过 `sharedFieldReferences` 实现引用计数，而不是每次都深拷贝。

### 14.1 修改文件：`beacon-chain/state/state-native/state_trie.go`

需要修改以下 4 个位置：

#### 14.1.1 修改 `electraFields` 数组（约 L98-109）

在 `electraFields` 数组中添加 term deposit 字段的 FieldIndex：

```go
electraFields = append(
    denebFields,
    types.DepositRequestsStartIndex,
    types.DepositBalanceToConsume,
    types.ExitBalanceToConsume,
    types.EarliestExitEpoch,
    types.ConsolidationBalanceToConsume,
    types.EarliestConsolidationEpoch,
    types.PendingDeposits,
    types.PendingPartialWithdrawals,
    types.PendingConsolidations,
    // ========== 新增 term deposit 字段 ==========
    types.TermDeposits,
    types.PendingTermDeposits,
    types.NextTermDepositId,
    types.PendingTermWithdrawals,
    types.TermDepositPenaltyPool,
    types.TermDepositPenaltyPoolLastDistEpoch,
)
```

#### 14.1.2 修改 `sharedFieldRefCount` 常量（约 L117-125）

只有**切片类型字段**需要共享引用计数：

- `TermDeposits` ✓（切片）
- `PendingTermDeposits` ✓（切片）
- `NextTermDepositId` ✗（uint64，不需要）
- `PendingTermWithdrawals` ✓（切片）
- `TermDepositPenaltyPool` ✗（uint64，不需要）
- `TermDepositPenaltyPoolLastDistEpoch` ✗（primitives.Epoch，不需要）

因此需要增加 **3 个**共享引用：

```go
const (
    phase0SharedFieldRefCount    = 5
    altairSharedFieldRefCount    = 5
    bellatrixSharedFieldRefCount = 6
    capellaSharedFieldRefCount   = 7
    denebSharedFieldRefCount     = 7
    electraSharedFieldRefCount   = 13  // 原 10 + 3 = 13
    fuluSharedFieldRefCount      = 14  // 原 11 + 3 = 14
)
```

#### 14.1.3 修改 `InitializeFromProtoUnsafeElectra()` 函数

**a) 在结构体初始化中添加字段赋值（约 L590 后）：**

```go
    pendingDeposits:                   st.PendingDeposits,
    pendingPartialWithdrawals:         st.PendingPartialWithdrawals,
    pendingConsolidations:             st.PendingConsolidations,
    // ========== 新增 term deposit 字段 ==========
    termDeposits:                            st.TermDeposits,
    pendingTermDeposits:                     st.PendingTermDeposits,
    nextTermDepositId:                       st.NextTermDepositId,
    pendingTermWithdrawals:                  st.PendingTermWithdrawals,
    termDepositPenaltyPool:                  st.TermDepositPenaltyPool,
    termDepositPenaltyPoolLastDistEpoch:     st.TermDepositPenaltyPoolLastDistributionEpoch,
```

**b) 在 sharedFieldReferences 初始化中添加（约 L627-629 后）：**

```go
    b.sharedFieldReferences[types.PendingDeposits] = stateutil.NewRef(1)
    b.sharedFieldReferences[types.PendingPartialWithdrawals] = stateutil.NewRef(1)
    b.sharedFieldReferences[types.PendingConsolidations] = stateutil.NewRef(1)
    // ========== 新增 term deposit 共享引用 ==========
    b.sharedFieldReferences[types.TermDeposits] = stateutil.NewRef(1)
    b.sharedFieldReferences[types.PendingTermDeposits] = stateutil.NewRef(1)
    b.sharedFieldReferences[types.PendingTermWithdrawals] = stateutil.NewRef(1)
```

#### 14.1.4 修改 `InitializeFromProtoUnsafeFulu()` 函数

同样的修改，在 Fulu 初始化函数中：

**a) 结构体赋值部分添加字段：**

```go
    termDeposits:                            st.TermDeposits,
    pendingTermDeposits:                     st.PendingTermDeposits,
    nextTermDepositId:                       st.NextTermDepositId,
    pendingTermWithdrawals:                  st.PendingTermWithdrawals,
    termDepositPenaltyPool:                  st.TermDepositPenaltyPool,
    termDepositPenaltyPoolLastDistEpoch:     st.TermDepositPenaltyPoolLastDistributionEpoch,
```

**b) sharedFieldReferences 初始化添加：**

```go
    b.sharedFieldReferences[types.TermDeposits] = stateutil.NewRef(1)
    b.sharedFieldReferences[types.PendingTermDeposits] = stateutil.NewRef(1)
    b.sharedFieldReferences[types.PendingTermWithdrawals] = stateutil.NewRef(1)
```

#### 14.1.5 修改 `Copy()` 函数

在 `Copy()` 函数的结构体初始化部分（约 L762-824），添加 term deposit 字段：

```go
dst := &BeaconState{
    // ... 现有字段 ...

    // 在 pendingConsolidations 之后添加（约 L798 后）
    pendingConsolidations:      b.pendingConsolidations,

    // ========== 新增 term deposit 字段 ==========
    termDeposits:                            b.termDeposits,
    pendingTermDeposits:                     b.pendingTermDeposits,
    nextTermDepositId:                       b.nextTermDepositId,
    pendingTermWithdrawals:                  b.pendingTermWithdrawals,
    termDepositPenaltyPool:                  b.termDepositPenaltyPool,
    termDepositPenaltyPoolLastDistEpoch:     b.termDepositPenaltyPoolLastDistEpoch,
    // ... 其他字段 ...
}
```

> **关于 sharedFieldReferences 的自动处理**：
>
> `Copy()` 函数中 L852-855 的循环会自动处理所有共享引用：
>
> ```go
> for field, ref := range b.sharedFieldReferences {
>     ref.AddRef()
>     dst.sharedFieldReferences[field] = ref
> }
> ```
>
> 这意味着：
>
> 1. 你**不需要**在 `Copy()` 中手动调用 `AddRef()`
> 2. 只要在 `InitializeFromProtoUnsafe*` 函数中正确初始化了 `sharedFieldReferences`
> 3. 并在 setter 函数中正确使用了 `sharedFieldReferences`（参见第 8 节）
> 4. 系统就会自动处理引用计数

---

## 15. Genesis State 初始化修改

### 15.1 修改文件：`runtime/interop/premine-state.go`

在 `empty()` 函数中，为 Electra/Fulu genesis 状态初始化 term deposit 字段：

```go
// 在 case version.Electra 和 case version.Fulu 之后，添加以下初始化
// （在 return e.Copy(), nil 之前）

if s.Version >= version.Electra {
    // 初始化 term deposit 字段
    if err = e.SetTermDeposits(make([]*ethpb.TermDeposit, 0)); err != nil {
        return nil, err
    }
    if err = e.SetPendingTermDeposits(make([]*ethpb.PendingTermDeposit, 0)); err != nil {
        return nil, err
    }
    if err = e.SetNextTermDepositId(0); err != nil {
        return nil, err
    }
    if err = e.SetPendingTermWithdrawals(make([]*ethpb.TermWithdrawalRequest, 0)); err != nil {
        return nil, err
    }
    if err = e.SetTermDepositPenaltyPool(0); err != nil {
        return nil, err
    }
    if err = e.SetTermDepositPenaltyPoolLastDistributionEpoch(0); err != nil {
        return nil, err
    }
}
```

---

## 16. 测试工具修改

### 16.1 修改文件：`testing/util/electra_state.go`

在 `emptyGenesisStateElectra()` 函数中添加 term deposit 字段初始化：

```go
// 在创建空的 Electra genesis state 时，需要初始化 term deposit 字段

func emptyGenesisStateElectra() (state.BeaconState, error) {
    st := &ethpb.BeaconStateElectra{
        // ... 其他字段 ...

        // Term deposit 字段初始化
        TermDeposits:                              make([]*ethpb.TermDeposit, 0),
        PendingTermDeposits:                       make([]*ethpb.PendingTermDeposit, 0),
        NextTermDepositId:                         0,
        PendingTermWithdrawals:                    make([]*ethpb.TermWithdrawalRequest, 0),
        TermDepositPenaltyPool:                    0,
        TermDepositPenaltyPoolLastDistributionEpoch: 0,
    }
    return state_native.InitializeFromProtoElectra(st)
}
```

### 16.2 修改文件：`testing/util/fulu_state.go`（如果存在）

类似地为 Fulu 状态添加 term deposit 字段初始化。

---

## 17. FieldCount 常量更新（关键）

> **重要**：这是编译必需的修改。`BeaconStateFieldCount` 常量用于验证状态字段数量，
> 如果不更新会导致状态初始化失败或哈希计算错误。

### 17.1 修改文件：`config/params/mainnet_config.go`

新增 6 个 term deposit 字段后，需要更新 Electra 和 Fulu 的状态字段计数：

```go
// 修改前（第 198-199 行）
BeaconStateElectraFieldCount:   37,
BeaconStateFuluFieldCount:      38,

// 修改后
BeaconStateElectraFieldCount:   43,  // 37 + 6 term deposit fields
BeaconStateFuluFieldCount:      44,  // 38 + 6 term deposit fields
```

**字段计数说明**：
- 原 Electra 字段数：37
- 新增 6 个字段：
  1. TermDeposits
  2. PendingTermDeposits
  3. NextTermDepositId
  4. PendingTermWithdrawals
  5. TermDepositPenaltyPool
  6. TermDepositPenaltyPoolLastDistEpoch
- 新 Electra 字段数：43
- 新 Fulu 字段数：44（在 Electra 基础上 +1 ProposerLookahead）

### 17.2 验证方法

可以在 `types/types.go` 中检查 `RealPosition()` 函数确认最大位置：
- TermDepositPenaltyPoolLastDistEpoch 位置是 43（基于0的索引）
- 因此总字段数应为 44（Electra）或 45（Fulu with ProposerLookahead at 37）

**注意**：实际 RealPosition 值请参考 `beacon-chain/state/state-native/types/types.go` 中的定义。

---

## 18. Hash Tree Root 计算修改（关键）

> **重要**：必须在 `hasher.go` 中添加 term deposit 字段的哈希计算，
> 否则状态根计算会出错，导致共识失败。

### 18.1 修改文件：`beacon-chain/state/state-native/hasher.go`

在 `computeFieldRoots` 函数的 Electra 版本检查块中，PendingConsolidations 之后添加 term deposit 字段的哈希计算：

```go
// 在 version.Electra 块内，PendingConsolidations 处理之后添加（约第 320 行之后）

// TermDeposits root.
tdRoot, err := stateutil.TermDepositsRoot(state.termDeposits)
if err != nil {
    return nil, errors.Wrap(err, "could not compute term deposits merkleization")
}
fieldRoots[types.TermDeposits.RealPosition()] = tdRoot[:]

// PendingTermDeposits root.
ptdRoot, err := stateutil.PendingTermDepositsRoot(state.pendingTermDeposits)
if err != nil {
    return nil, errors.Wrap(err, "could not compute pending term deposits merkleization")
}
fieldRoots[types.PendingTermDeposits.RealPosition()] = ptdRoot[:]

// NextTermDepositId root.
ntdiRoot := ssz.Uint64Root(state.nextTermDepositId)
fieldRoots[types.NextTermDepositId.RealPosition()] = ntdiRoot[:]

// PendingTermWithdrawals root.
ptwRoot, err := stateutil.PendingTermWithdrawalsRoot(state.pendingTermWithdrawals)
if err != nil {
    return nil, errors.Wrap(err, "could not compute pending term withdrawals merkleization")
}
fieldRoots[types.PendingTermWithdrawals.RealPosition()] = ptwRoot[:]

// TermDepositPenaltyPool root.
tdppRoot := ssz.Uint64Root(state.termDepositPenaltyPool)
fieldRoots[types.TermDepositPenaltyPool.RealPosition()] = tdppRoot[:]

// TermDepositPenaltyPoolLastDistEpoch root.
tdppldeRoot := ssz.Uint64Root(uint64(state.termDepositPenaltyPoolLastDistEpoch))
fieldRoots[types.TermDepositPenaltyPoolLastDistEpoch.RealPosition()] = tdppldeRoot[:]
```

### 18.2 完整代码位置参考

在 `hasher.go` 中找到以下代码块：

```go
if state.version >= version.Electra {
    // ... existing Electra fields ...

    // PendingConsolidations root.
    pcRoot, err := stateutil.PendingConsolidationsRoot(state.pendingConsolidations)
    if err != nil {
        return nil, errors.Wrap(err, "could not compute pending consolidations merkleization")
    }
    fieldRoots[types.PendingConsolidations.RealPosition()] = pcRoot[:]

    // ===== 在此处添加上述 term deposit 字段哈希计算 =====
}
```

---

## 19. StateUtil Root 函数（关键）

> **重要**：需要为 3 个切片类型的 term deposit 字段创建 SSZ root 计算函数。

### 19.1 新建文件：`beacon-chain/state/stateutil/term_deposits_root.go`

```go
package stateutil

import (
	fieldparams "github.com/OffchainLabs/prysm/v6/config/fieldparams"
	"github.com/OffchainLabs/prysm/v6/encoding/ssz"
	ethpb "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1"
)

// TermDepositsRoot computes the SSZ hash tree root of term deposits slice.
func TermDepositsRoot(slice []*ethpb.TermDeposit) ([32]byte, error) {
	return ssz.SliceRoot(slice, fieldparams.TermDepositsLimit)
}
```

### 19.2 新建文件：`beacon-chain/state/stateutil/pending_term_deposits_root.go`

```go
package stateutil

import (
	fieldparams "github.com/OffchainLabs/prysm/v6/config/fieldparams"
	"github.com/OffchainLabs/prysm/v6/encoding/ssz"
	ethpb "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1"
)

// PendingTermDepositsRoot computes the SSZ hash tree root of pending term deposits slice.
func PendingTermDepositsRoot(slice []*ethpb.PendingTermDeposit) ([32]byte, error) {
	return ssz.SliceRoot(slice, fieldparams.PendingTermDepositsLimit)
}
```

### 19.3 新建文件：`beacon-chain/state/stateutil/pending_term_withdrawals_root.go`

```go
package stateutil

import (
	fieldparams "github.com/OffchainLabs/prysm/v6/config/fieldparams"
	"github.com/OffchainLabs/prysm/v6/encoding/ssz"
	ethpb "github.com/OffchainLabs/prysm/v6/proto/prysm/v1alpha1"
)

// PendingTermWithdrawalsRoot computes the SSZ hash tree root of pending term withdrawals slice.
func PendingTermWithdrawalsRoot(slice []*ethpb.TermWithdrawalRequest) ([32]byte, error) {
	return ssz.SliceRoot(slice, fieldparams.PendingTermWithdrawalsLimit)
}
```

---

## 20. FieldParams 限制常量（关键）

> **重要**：SSZ 编码需要这些限制常量来计算正确的 Merkle root。

### 20.1 修改文件：`config/fieldparams/mainnet.go`

在现有常量之后添加 term deposit 相关限制：

```go
const (
    // ... 现有常量 ...

    PendingConsolidationsLimit            = 262144            // Maximum number of pending consolidations in the beacon state.

    // Term deposit 相关限制（在 PendingConsolidationsLimit 之后添加）
    TermDepositsLimit                     = 1048576           // Maximum number of term deposits per validator in the beacon state.
    PendingTermDepositsLimit              = 134217728         // Maximum number of pending term deposits in the beacon state.
    PendingTermWithdrawalsLimit           = 134217728         // Maximum number of pending term withdrawals in the beacon state.

    // ... 其他常量 ...
)
```

**限制值说明**：
- `TermDepositsLimit`: 建议 2^20 = 1048576，支持大量存单
- `PendingTermDepositsLimit`: 与 `PendingDepositsLimit` 相同 (2^27 = 134217728)
- `PendingTermWithdrawalsLimit`: 与 `PendingPartialWithdrawalsLimit` 相同 (2^27 = 134217728)

### 20.2 修改文件：`config/fieldparams/minimal.go`（如果需要支持测试网）

同样添加相应常量，可以使用较小的值用于测试：

```go
const (
    // ... 现有常量 ...

    // Term deposit 相关限制
    TermDepositsLimit                     = 64                // Minimal: smaller limit for testing
    PendingTermDepositsLimit              = 64                // Minimal: smaller limit for testing
    PendingTermWithdrawalsLimit           = 64                // Minimal: smaller limit for testing
)
```

---

## 21. State Proto 转换函数修改

> **重要**：`getters_state.go` 中的 `ToProtoUnsafe()` 和 `ToProto()` 函数用于将内部状态
> 转换为 protobuf 格式。如果不添加 term deposit 字段，序列化/反序列化时会丢失数据。

### 21.1 修改文件：`beacon-chain/state/state-native/getters_state.go`

#### 21.1.1 修改 `ToProtoUnsafe()` 函数 - Electra case

在 `case version.Electra:` 的返回结构中，`PendingConsolidations` 之后添加：

```go
case version.Electra:
    return &ethpb.BeaconStateElectra{
        // ... 现有字段 ...
        PendingDeposits:               b.pendingDeposits,
        PendingPartialWithdrawals:     b.pendingPartialWithdrawals,
        PendingConsolidations:         b.pendingConsolidations,
        // ========== 新增 term deposit 字段 ==========
        TermDeposits:                                b.termDeposits,
        PendingTermDeposits:                         b.pendingTermDeposits,
        NextTermDepositId:                           b.nextTermDepositId,
        PendingTermWithdrawals:                      b.pendingTermWithdrawals,
        TermDepositPenaltyPool:                      b.termDepositPenaltyPool,
        TermDepositPenaltyPoolLastDistributionEpoch: b.termDepositPenaltyPoolLastDistEpoch,
    }
```

#### 21.1.2 修改 `ToProtoUnsafe()` 函数 - Fulu case

在 `case version.Fulu:` 的返回结构中，`ProposerLookahead` 之后添加：

```go
case version.Fulu:
    lookahead := make([]uint64, len(b.proposerLookahead))
    for i, v := range b.proposerLookahead {
        lookahead[i] = uint64(v)
    }
    return &ethpb.BeaconStateFulu{
        // ... 现有字段 ...
        PendingConsolidations:                       b.pendingConsolidations,
        ProposerLookahead:                           lookahead,
        // ========== 新增 term deposit 字段 ==========
        TermDeposits:                                b.termDeposits,
        PendingTermDeposits:                         b.pendingTermDeposits,
        NextTermDepositId:                           b.nextTermDepositId,
        PendingTermWithdrawals:                      b.pendingTermWithdrawals,
        TermDepositPenaltyPool:                      b.termDepositPenaltyPool,
        TermDepositPenaltyPoolLastDistributionEpoch: b.termDepositPenaltyPoolLastDistEpoch,
    }
```

#### 21.1.3 修改 `ToProto()` 函数 - Electra case

在 `ToProto()` 函数的 `case version.Electra:` 中添加（使用 `Val` 方法获取副本）：

```go
case version.Electra:
    return &ethpb.BeaconStateElectra{
        // ... 现有字段 ...
        PendingDeposits:               b.pendingDepositsVal(),
        PendingPartialWithdrawals:     b.pendingPartialWithdrawalsVal(),
        PendingConsolidations:         b.pendingConsolidationsVal(),
        // ========== 新增 term deposit 字段 ==========
        TermDeposits:                                b.termDepositsVal(),
        PendingTermDeposits:                         b.pendingTermDepositsVal(),
        NextTermDepositId:                           b.nextTermDepositId,
        PendingTermWithdrawals:                      b.pendingTermWithdrawalsVal(),
        TermDepositPenaltyPool:                      b.termDepositPenaltyPool,
        TermDepositPenaltyPoolLastDistributionEpoch: b.termDepositPenaltyPoolLastDistEpoch,
    }
```

#### 21.1.4 修改 `ToProto()` 函数 - Fulu case

在 `ToProto()` 函数的 `case version.Fulu:` 中添加：

```go
case version.Fulu:
    lookahead := make([]uint64, len(b.proposerLookahead))
    for i, v := range b.proposerLookahead {
        lookahead[i] = uint64(v)
    }
    return &ethpb.BeaconStateFulu{
        // ... 现有字段 ...
        PendingConsolidations:                       b.pendingConsolidationsVal(),
        ProposerLookahead:                           lookahead,
        // ========== 新增 term deposit 字段 ==========
        TermDeposits:                                b.termDepositsVal(),
        PendingTermDeposits:                         b.pendingTermDepositsVal(),
        NextTermDepositId:                           b.nextTermDepositId,
        PendingTermWithdrawals:                      b.pendingTermWithdrawalsVal(),
        TermDepositPenaltyPool:                      b.termDepositPenaltyPool,
        TermDepositPenaltyPoolLastDistributionEpoch: b.termDepositPenaltyPoolLastDistEpoch,
    }
```

### 21.2 `ToProtoUnsafe()` vs `ToProto()` 区别

| 方法 | 特点 | 使用场景 |
|------|------|----------|
| `ToProtoUnsafe()` | 直接返回内部指针，不复制数据 | 性能敏感场景，调用方保证不修改 |
| `ToProto()` | 使用 `*Val()` 方法返回数据副本 | 需要安全副本的场景 |

**注意**：对于 `uint64` 类型字段（如 `NextTermDepositId`、`TermDepositPenaltyPool`），
两个函数都直接使用字段值，因为基本类型是值传递。

---

## 22. rootSelector() 函数修改（关键）

**文件**：`beacon-chain/state/state-native/state_trie.go`

### 22.1 问题描述

`rootSelector()` 函数是 state hash tree root 计算的关键函数，用于根据字段索引选择对应的 root 计算方法。
如果缺少 term deposit 字段的 case 分支，会导致 "invalid field index provided" 错误。

### 22.2 修改位置

在 `rootSelector()` 函数的 switch 语句末尾，`case types.ProposerLookahead:` 之后添加：

```go
func (b *BeaconState) rootSelector(ctx context.Context, field types.FieldIndex) ([32]byte, error) {
    // ... 现有 case 分支 ...
    case types.ProposerLookahead:
        return stateutil.ProposerLookaheadRoot(b.proposerLookahead)
    // ========== 新增 Term Deposit 字段的 case 分支 ==========
    case types.TermDeposits:
        return stateutil.TermDepositsRoot(b.termDeposits)
    case types.PendingTermDeposits:
        return stateutil.PendingTermDepositsRoot(b.pendingTermDeposits)
    case types.NextTermDepositId:
        return ssz.Uint64Root(b.nextTermDepositId), nil
    case types.PendingTermWithdrawals:
        return stateutil.PendingTermWithdrawalsRoot(b.pendingTermWithdrawals)
    case types.TermDepositPenaltyPool:
        return ssz.Uint64Root(b.termDepositPenaltyPool), nil
    case types.TermDepositPenaltyPoolLastDistEpoch:
        return ssz.Uint64Root(uint64(b.termDepositPenaltyPoolLastDistEpoch)), nil
    // ========== 新增结束 ==========
    }
    return [32]byte{}, errors.New("invalid field index provided")
}
```

### 22.3 注意事项

- 切片类型字段使用 `stateutil.*Root()` 函数
- `uint64` 类型字段使用 `ssz.Uint64Root()`
- `primitives.Epoch` 类型需要转换为 `uint64`

---

## 23. beaconStateMarshalable 结构体修改

**文件**：`beacon-chain/state/state-native/beacon_state.go`

### 23.1 问题描述

`beaconStateMarshalable` 结构体用于 JSON 序列化，如果缺少 term deposit 字段，
`MarshalJSON()` 方法导出的 JSON 将不包含这些字段。

### 23.2 修改 beaconStateMarshalable 结构体

在结构体定义中添加 6 个 term deposit 字段：

```go
type beaconStateMarshalable struct {
    // ... 现有字段 ...
    PendingDeposits                              []*ethpb.PendingDeposit           `json:"pending_deposits"`
    PendingPartialWithdrawals                    []*ethpb.PendingPartialWithdrawal `json:"pending_partial_withdrawals"`
    PendingConsolidations                        []*ethpb.PendingConsolidation     `json:"pending_consolidations"`
    ProposerLookahead                            []primitives.ValidatorIndex       `json:"proposer_look_ahead"`
    // ========== 新增 Term Deposit 字段 ==========
    TermDeposits                                 []*ethpb.TermDeposit              `json:"term_deposits"`
    PendingTermDeposits                          []*ethpb.PendingTermDeposit       `json:"pending_term_deposits"`
    NextTermDepositId                            uint64                            `json:"next_term_deposit_id"`
    PendingTermWithdrawals                       []*ethpb.TermWithdrawalRequest    `json:"pending_term_withdrawals"`
    TermDepositPenaltyPool                       uint64                            `json:"term_deposit_penalty_pool"`
    TermDepositPenaltyPoolLastDistributionEpoch  primitives.Epoch                  `json:"term_deposit_penalty_pool_last_distribution_epoch"`
    // ========== 新增结束 ==========
}
```

### 23.3 修改 MarshalJSON() 函数

在 `MarshalJSON()` 函数中添加字段赋值：

```go
func (b *BeaconState) MarshalJSON() ([]byte, error) {
    // ... 现有代码 ...
    marshalable := &beaconStateMarshalable{
        // ... 现有字段 ...
        PendingDeposits:                             b.pendingDeposits,
        PendingPartialWithdrawals:                   b.pendingPartialWithdrawals,
        PendingConsolidations:                       b.pendingConsolidations,
        ProposerLookahead:                           b.proposerLookahead,
        // ========== 新增 Term Deposit 字段 ==========
        TermDeposits:                                b.termDeposits,
        PendingTermDeposits:                         b.pendingTermDeposits,
        NextTermDepositId:                           b.nextTermDepositId,
        PendingTermWithdrawals:                      b.pendingTermWithdrawals,
        TermDepositPenaltyPool:                      b.termDepositPenaltyPool,
        TermDepositPenaltyPoolLastDistributionEpoch: b.termDepositPenaltyPoolLastDistEpoch,
        // ========== 新增结束 ==========
    }
    return json.Marshal(marshalable)
}
```

---

## 24. minimal_config.go Term Deposit 配置

**文件**：`config/params/minimal_config.go`

### 24.1 问题描述

`minimal_config.go` 用于测试环境，如果不设置 term deposit 相关配置，
会继承 mainnet 的配置值，这些值对于测试来说可能过大。

### 24.2 修改内容

在 `MinimalSpecConfig()` 函数中，"New Electra params" 部分之后添加：

```go
// New Electra params
minimalConfig.MinPerEpochChurnLimitElectra = 64000000000
minimalConfig.MaxPerEpochActivationExitChurnLimit = 128000000000
// ...

// ========== Term deposit params (minimal for testing) ==========
minimalConfig.TermDepositsLimit = 64
minimalConfig.PendingTermDepositsLimit = 64
minimalConfig.MaxTermDepositsPerValidator = 4
minimalConfig.DefaultTermDepositGracePeriod = 8    // 1 epoch
minimalConfig.MinTermDepositGracePeriod = 4        // half epoch
minimalConfig.MaxTermDepositGracePeriod = 64       // 8 epochs
minimalConfig.MinTermDepositDuration = 8           // 1 epoch
minimalConfig.MaxTermDepositDuration = 256         // 32 epochs
minimalConfig.EarlyWithdrawalPenaltyBaseBps = 100  // 1%
minimalConfig.MaxTermDepositRenewals = 0           // unlimited
minimalConfig.TermDepositPenaltyPoolDistributionInterval = 8 // every epoch
minimalConfig.TermDepositForkEpoch = 0             // genesis enabled
// ========== Term deposit params 结束 ==========

// Ethereum PoW parameters.
minimalConfig.DepositChainID = 5
```

### 24.3 配置对比

| 参数 | Mainnet | Minimal |
|------|---------|---------|
| TermDepositsLimit | 1,048,576 | 64 |
| PendingTermDepositsLimit | 65,536 | 64 |
| MaxTermDepositsPerValidator | 256 | 4 |
| DefaultTermDepositGracePeriod | 4,050 | 8 |
| MinTermDepositDuration | 4,050 | 8 |
| MaxTermDepositDuration | ~4.93M | 256 |

---

## 25. BUILD.bazel 文件修改清单（关键）

### 25.1 问题描述

Bazel 构建系统需要在 BUILD.bazel 文件中显式声明源文件和依赖项。
如果新增的 .go 文件没有添加到 BUILD.bazel，会导致编译时 "undefined" 错误。

### 25.2 需要修改的 BUILD.bazel 文件

#### 25.2.1 beacon-chain/state/stateutil/BUILD.bazel

添加 3 个新建的 root 计算文件：

```python
go_library(
    name = "go_default_library",
    srcs = [
        # ... 现有文件 ...
        "pending_partial_withdrawals_root.go",
        "pending_term_deposits_root.go",      # 新增
        "pending_term_withdrawals_root.go",   # 新增
        "proposer_lookahead_root.go",
        "term_deposits_root.go",              # 新增
        "reference.go",
        # ...
    ],
    # ...
)
```

#### 25.2.2 beacon-chain/state/state-native/BUILD.bazel

添加 getter 和 setter 文件：

```python
go_library(
    name = "go_default_library",
    srcs = [
        # ... 现有文件 ...
        "getters_sync_committee.go",
        "getters_term_deposit.go",    # 新增
        "getters_validator.go",
        # ...
        "setters_sync_committee.go",
        "setters_term_deposit.go",    # 新增
        "setters_validator.go",
        # ...
    ],
    # ...
)
```

#### 25.2.3 beacon-chain/core/helpers/BUILD.bazel

添加 term_deposit.go 文件：

```python
go_library(
    name = "go_default_library",
    srcs = [
        # ... 现有文件 ...
        "sync_committee.go",
        "term_deposit.go",    # 新增
        "validator_churn.go",
        # ...
    ],
    # ...
)
```

#### 25.2.4 beacon-chain/core/validators/BUILD.bazel

添加 log.go 文件和 logrus 依赖：

```python
go_library(
    name = "go_default_library",
    srcs = [
        "log.go",           # 新增（如果之前没有）
        "slashing.go",
        "validator.go",
    ],
    deps = [
        # ... 现有依赖 ...
        "@com_github_pkg_errors//:go_default_library",
        "@com_github_sirupsen_logrus//:go_default_library",  # 新增
    ],
)
```

#### 25.2.5 beacon-chain/core/electra/BUILD.bazel

添加 term_deposit.go 文件和 crypto/bls 依赖：

```python
go_library(
    name = "go_default_library",
    srcs = [
        # ... 现有文件 ...
        "registry_updates.go",
        "term_deposit.go",    # 新增
        "transition.go",
        # ...
    ],
    deps = [
        # ... 现有依赖 ...
        "//contracts/deposit:go_default_library",
        "//crypto/bls:go_default_library",        # 新增
        "//crypto/bls/common:go_default_library",
        # ...
    ],
)
```

### 25.3 BUILD.bazel 修改检查清单

| 文件 | 新增源文件 | 新增依赖 |
|------|-----------|---------|
| `beacon-chain/state/stateutil/BUILD.bazel` | 3 个 root 文件 | - |
| `beacon-chain/state/state-native/BUILD.bazel` | getters_term_deposit.go, setters_term_deposit.go | - |
| `beacon-chain/core/helpers/BUILD.bazel` | term_deposit.go | - |
| `beacon-chain/core/validators/BUILD.bazel` | log.go | logrus |
| `beacon-chain/core/electra/BUILD.bazel` | term_deposit.go | crypto/bls |

### 25.4 验证命令

修改 BUILD.bazel 后，使用以下命令验证编译：

```bash
# 验证 state 包
bazel build //beacon-chain/state/state-native:go_default_library

# 验证完整的 beacon-chain
bazel build //cmd/beacon-chain:beacon-chain
```

---

## 附录 A：完整修改检查清单

以下是本次 term deposit 改造需要修改的所有文件清单，用于最终验证：

### A.1 Proto 层（必须先完成，执行 bazel 编译生成 Go 代码）

| 文件 | 修改类型 | 状态 |
|------|---------|------|
| `proto/prysm/v1alpha1/term_deposit.proto` | 新建 | □ |
| `proto/prysm/v1alpha1/beacon_state.proto` | 新增字段 | □ |
| `proto/prysm/v1alpha1/BUILD.bazel` | SSZ 配置 | □ |

### A.2 配置层

| 文件 | 修改类型 | 状态 |
|------|---------|------|
| `config/params/config.go` | 新增配置参数定义 | □ |
| `config/params/mainnet_config.go` | 配置值 + **FieldCount** | □ |
| `config/fieldparams/mainnet.go` | **Limit 常量** | □ |
| `config/fieldparams/minimal.go` | Limit 常量（可选） | □ |

### A.3 State 类型层

| 文件 | 修改类型 | 状态 |
|------|---------|------|
| `beacon-chain/state/state-native/types/types.go` | FieldIndex 枚举 | □ |
| `beacon-chain/state/interfaces.go` | 接口方法声明 | □ |
| `beacon-chain/state/state-native/beacon_state.go` | struct 字段 | □ |

### A.4 State 实现层

| 文件 | 修改类型 | 状态 |
|------|---------|------|
| `beacon-chain/state/state-native/getters_term_deposit.go` | 新建 | □ |
| `beacon-chain/state/state-native/setters_term_deposit.go` | 新建 | □ |
| `beacon-chain/state/state-native/state_trie.go` | Copy(), Init*, **rootSelector()** | □ |
| `beacon-chain/state/state-native/hasher.go` | **哈希计算** | □ |
| `beacon-chain/state/state-native/getters_state.go` | **Proto转换函数** | □ |
| `beacon-chain/state/state-native/beacon_state.go` | **beaconStateMarshalable** | □ |

### A.5 StateUtil 层

| 文件 | 修改类型 | 状态 |
|------|---------|------|
| `beacon-chain/state/stateutil/term_deposits_root.go` | **新建** | □ |
| `beacon-chain/state/stateutil/pending_term_deposits_root.go` | **新建** | □ |
| `beacon-chain/state/stateutil/pending_term_withdrawals_root.go` | **新建** | □ |

### A.6 Core 逻辑层

| 文件 | 修改类型 | 状态 |
|------|---------|------|
| `beacon-chain/core/electra/term_deposit.go` | 新建 | □ |
| `beacon-chain/core/helpers/term_deposit.go` | 新建 | □ |
| `beacon-chain/core/validators/log.go` | 新建 | □ |
| `beacon-chain/core/electra/transition.go` | 调用处理 | □ |
| `beacon-chain/core/electra/effective_balance_updates.go` | 有效余额计算 | □ |
| `beacon-chain/core/validators/validator.go` | Slashing 处理 | □ |

### A.7 状态升级层

| 文件 | 修改类型 | 状态 |
|------|---------|------|
| `beacon-chain/core/electra/upgrade.go` | 初始化字段 | □ |
| `beacon-chain/core/fulu/upgrade.go` | 继承字段 | □ |

### A.8 初始化层

| 文件 | 修改类型 | 状态 |
|------|---------|------|
| `runtime/interop/premine-state.go` | Genesis 初始化 | □ |
| `testing/util/electra_state.go` | 测试工具 | □ |
| `testing/util/fulu_state.go` | 测试工具（可选） | □ |

### A.9 BUILD.bazel 层（关键）

| 文件 | 修改类型 | 状态 |
|------|---------|------|
| `beacon-chain/state/stateutil/BUILD.bazel` | 新增 3 个 root 文件 | □ |
| `beacon-chain/state/state-native/BUILD.bazel` | 新增 getter/setter 文件 | □ |
| `beacon-chain/core/helpers/BUILD.bazel` | 新增 term_deposit.go | □ |
| `beacon-chain/core/validators/BUILD.bazel` | 新增 log.go + logrus 依赖 | □ |
| `beacon-chain/core/electra/BUILD.bazel` | 新增 term_deposit.go + bls 依赖 | □ |

### A.10 配置层（补充）

| 文件 | 修改类型 | 状态 |
|------|---------|------|
| `config/params/minimal_config.go` | Term Deposit 测试配置 | □ |

---

## 附录 B：注意事项

1. **Proto 字段编号**：确保新添加的字段编号（12010-12015）不与现有字段冲突
2. **版本检查**：所有 getter/setter 方法都需要检查 `b.version < version.Electra`
3. **引用计数**：修改 state 字段时需要正确处理 `sharedFieldReferences`
4. **类型转换**：注意 `primitives.ValidatorIndex` 和 `primitives.Epoch` 与 `uint64` 之间的转换
5. **并发安全**：所有 state 操作都需要正确使用 `lock.Lock()` 或 `lock.RLock()`
6. **Genesis 生成**：使用 prysmctl 生成 genesis.ssz 前，确保所有初始化代码已正确修改
7. **状态升级**：链升级时（Deneb→Electra、Electra→Fulu），需要正确初始化/继承 term deposit 字段

---

## 附录 C：后续工作

1. **ETH1 日志处理**：实现 `ProcessTermDepositLog` 和 `ProcessTermWithdrawalLog`
2. **测试用例**：编写完整的单元测试和集成测试
3. **监控指标**：添加 Prometheus 指标
4. **文档更新**：更新用户文档和 API 文档
