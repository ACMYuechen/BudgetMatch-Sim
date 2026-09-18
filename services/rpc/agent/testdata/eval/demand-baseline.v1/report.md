# Agent 需求快照选品与核验回放

合成事实、公开回归样例、待独立人工复核；不是线上 A/B、真实分类质量或性能验收。

- 数据：demand-v1；SHA-256：`b98efd9240db2af33b6b8dffe2604913a0d1e5e0b4c4fed10bbd3db143843ac5`。
- 协议：same-snapshot-demand-replay-v1；SHA-256：`6a8c9e81441a199210c40103465bb1ec622adfd88caf3c12a92f2a3745307f12`。
- 代码标记：fa98f6f+M4.3a-worktree（调用者提供，未认证）。
- 环境：linux / amd64，go1.26.8，GOMAXPROCS=2；并发 1，无显式预热、固定策略/用例顺序。

## 同快照选品

旧贪心是直接调用 BundleSelector 的算法对照，不是旧 Recommend/API 已支持需求：只传它支持的同一预算/件数投影，再用完整需求统一评估；生产 Demand 拒绝保护不变。所有策略读取相同完整候选快照，窗口裁剪是被测策略的一部分；不做检索、复核、规划或模型调用。独立穷举最多 8 SKU / 255 个非空子集，仅证明该快照的可行性与文档化目标，不证明真实全库最优。

| 策略 | 可满足任务成功 | 非空结果硬约束违规 | 必需覆盖 | 可选覆盖 | 可行样例穷举最优命中 | 受限漏解/全部漏解 |
| --- | --- | --- | --- | --- | --- | --- |
| legacy_greedy | 8/11（72.73%） | 8/16（50.00%） | 14/21（66.67%） | 2/3（66.67%） | 7/11（63.64%） | 0/3 |
| beam_default | 11/11（100.00%） | 0/11（0.00%） | 14/21（66.67%） | 3/3（100.00%） | 11/11（100.00%） | 0/0 |
| beam_width_1 | 10/11（90.91%） | 0/10（0.00%） | 12/21（57.14%） | 3/3（100.00%） | 10/11（90.91%） | 1/1 |
| beam_window_1 | 7/11（63.64%） | 0/7（0.00%） | 6/21（28.57%） | 0/3（0.00%） | 4/11（36.36%） | 4/4 |
| beam_expansions_1 | 7/11（63.64%） | 0/7（0.00%） | 6/21（28.57%） | 0/3（0.00%） | 4/11（36.36%） | 4/4 |

| 策略 | 窗口/宽度/展开上限/ms | 输入快照数 | 展开尝试总数 | 本机 P50/P95（ms） |
| --- | --- | --- | --- | --- |
| legacy_greedy | 不适用 | 37 | 未插桩（null） | 0.0013/0.0136 |
| beam_default | 32/32/8192/100 | 37 | 44 | 0.0039/0.1646 |
| beam_width_1 | 32/1/8192/100 | 37 | 39 | 0.0139/0.2289 |
| beam_window_1 | 1/32/8192/100 | 37 | 7 | 0.0051/0.0335 |
| beam_expansions_1 | 32/32/1/100 | 37 | 13 | 0.0042/0.0103 |

### 保留的失败与非最优样例

| 用例 | 策略 | 有解 | 所选 SKU | 最优 SKU | 漏解/违规 |
| --- | --- | --- | --- | --- | --- |
| greedy-trap | legacy_greedy | true | premium-key | cheap-key, mouse | invalid_bundle missing_required |
| excluded-category | legacy_greedy | true | cheap-key, headphones, mouse | cheap-key, mouse | invalid_bundle excluded_category |
| unknown-cannot-prove-exclusion | legacy_greedy | true | cheap-key, unknown | cheap-key | invalid_bundle unknown_with_exclusions |
| missing-category | legacy_greedy | false | cheap-key, mouse |  |  missing_required |
| budget-infeasible | legacy_greedy | false | cheap-key |  |  missing_required |
| explicit-value | legacy_greedy | true | premium-key | cheap-key |   |
| pair-over-budget | legacy_greedy | false | premium-key |  |  missing_required |
| filtered-before-recheck | legacy_greedy | false | mouse |  |  missing_required |
| all-excluded | legacy_greedy | false | headphones, unknown |  |  unknown_with_exclusions, excluded_category |
| narrow-pruning-trap | beam_width_1 | true |  | b-cheap-key, c-required-mouse | beam_pruning  |
| greedy-trap | beam_window_1 | true |  | cheap-key, mouse | candidate_window  |
| narrow-pruning-trap | beam_window_1 | true |  | b-cheap-key, c-required-mouse | candidate_window  |
| simple-pair | beam_window_1 | true |  | cheap-key, mouse | candidate_window  |
| excluded-category | beam_window_1 | true | cheap-key | cheap-key, mouse |   |
| optional-coverage | beam_window_1 | true | cheap-key | cheap-key, light, mouse |   |
| prerecorded-rank | beam_window_1 | true | cheap-key | premium-key |   |
| recheck-alternatives | beam_window_1 | true |  | cheap-key, mouse | candidate_window  |
| greedy-trap | beam_expansions_1 | true |  | cheap-key, mouse | expansion_limit  |
| narrow-pruning-trap | beam_expansions_1 | true |  | b-cheap-key, c-required-mouse | expansion_limit  |
| simple-pair | beam_expansions_1 | true |  | cheap-key, mouse | expansion_limit  |
| excluded-category | beam_expansions_1 | true | cheap-key | cheap-key, mouse |   |
| optional-coverage | beam_expansions_1 | true | cheap-key | cheap-key, light, mouse |   |
| prerecorded-rank | beam_expansions_1 | true | cheap-key | premium-key |   |
| recheck-alternatives | beam_expansions_1 | true |  | cheap-key, mouse | expansion_limit  |

### 默认 Beam 相对对照的任务成功变化

| 对照 | 改善 | 退化 | 持平 |
| --- | --- | --- | --- |
| legacy_greedy | greedy-trap, excluded-category, unknown-cannot-prove-exclusion |  | narrow-pruning-trap, simple-pair, missing-category, budget-infeasible, empty-snapshot, unknown-broad-demand, invalid-facts-filtered, optional-coverage, prerecorded-rank, explicit-value, recheck-alternatives, pair-over-budget, filtered-before-recheck, all-excluded |
| beam_width_1 | narrow-pruning-trap |  | greedy-trap, simple-pair, excluded-category, unknown-cannot-prove-exclusion, missing-category, budget-infeasible, empty-snapshot, unknown-broad-demand, invalid-facts-filtered, optional-coverage, prerecorded-rank, explicit-value, recheck-alternatives, pair-over-budget, filtered-before-recheck, all-excluded |
| beam_window_1 | greedy-trap, narrow-pruning-trap, simple-pair, recheck-alternatives |  | excluded-category, unknown-cannot-prove-exclusion, missing-category, budget-infeasible, empty-snapshot, unknown-broad-demand, invalid-facts-filtered, optional-coverage, prerecorded-rank, explicit-value, pair-over-budget, filtered-before-recheck, all-excluded |
| beam_expansions_1 | greedy-trap, narrow-pruning-trap, simple-pair, recheck-alternatives |  | excluded-category, unknown-cannot-prove-exclusion, missing-category, budget-infeasible, empty-snapshot, unknown-broad-demand, invalid-facts-filtered, optional-coverage, prerecorded-rank, explicit-value, pair-over-budget, filtered-before-recheck, all-excluded |

## 独立 Mall 执行回放

运行生产 NewMall Executor、分类核验、两次选品及最终检查，外部接口由内存快照替身实现。没有真实 Mall 搜索、gRPC 传输、JWT 验签、数据库、索引或库存预占；此层不与裸选择器的延迟混算。

快照变化 9 例，故障 9 例；预期终态 18/18（100.00%）；故障闭合 9/9（100.00%）；非空硬约束违规 0/6（0.00%）；最终检索快照可满足成功 6/7（85.71%）。Provider 读取 18，分类核验调用 29，展开尝试 63。快照用例本机 P50/P95=0.0388/0.2785 ms。

| 用例 | 注入阶段/故障 | 实际/预期 | 最终检索快照有解 | 所选 SKU | 读取/核验 | 初选/重选展开 | 门禁问题 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| stable |   | complete/complete | true | cheap-key, mouse | 1/2 | 6/6 |  |
| repriced |   | complete/complete | true | alt-key, mouse | 1/2 | 6/6 |  |
| off-shelf |   | complete/complete | true | alt-key, mouse | 1/2 | 6/3 |  |
| category-changed |   | complete/complete | true | alt-key, mouse | 1/2 | 6/3 |  |
| category-withdrawn |   | complete/complete | true | alt-key, mouse | 1/2 | 6/3 |  |
| required-lost |   | no_feasible_bundle/no_feasible_bundle | false |  | 1/2 | 6/0 |  |
| price-drop-recovers |   | complete/complete | true | mouse, premium-key | 1/2 | 3/3 |  |
| outside-window-not-revived |   | no_feasible_bundle/no_feasible_bundle | true |  | 1/2 | 0/0 |  |
| empty-no-certification |   | no_feasible_bundle/no_feasible_bundle | false |  | 1/0 | 0/0 |  |
| search-unavailable | search unavailable | unavailable/unavailable | true |  | 1/0 | 0/0 |  |
| initial-denied | initial_check permission_denied | permission_denied/permission_denied | true |  | 1/1 | 0/0 |  |
| initial-old-server | initial_check bad_contract | unavailable/unavailable | true |  | 1/1 | 0/0 |  |
| initial-missing | initial_check missing_fact | unavailable/unavailable | true |  | 1/1 | 0/0 |  |
| final-denied | final_check permission_denied | permission_denied/permission_denied | true |  | 1/2 | 0/0 |  |
| final-timeout | final_check deadline_exceeded | unavailable/unavailable | true |  | 1/2 | 0/0 |  |
| final-canceled | final_check canceled | canceled/canceled | true |  | 1/2 | 0/0 |  |
| final-missing | final_check missing_fact | unavailable/unavailable | true |  | 1/2 | 0/0 |  |
| final-revision-rollback | final_check revision_rollback | unsafe_result/unsafe_result | true |  | 1/2 | 0/0 |  |

机制门禁：true。不要求新策略必须获胜；旧贪心的分类不支持、主动收窄策略的漏解和复核窗口外降价导致的漏解均保留。门禁约束事实/数值安全、新路径需求安全、调用/资源边界和故障预期。实际触及时限属于本次测量不确定，不能静默通过。

## 口径、反例与未运行项

- 可满足成功的分母由独立穷举确定；所有失败留在分母。空结果不算成功。硬约束违规分母是非空结果，包括缺必需类别；空结果漏解另报，不能用零违规掩盖漏解。
- 必需/可选覆盖是逐例类别命中数的 micro 比率，包含不可满足输入。事实、排除或数值违规的结果覆盖归零；事实合法的部分必需覆盖仍记录，但不是任务成功。无适用样本的比率为 null。
- 穷举目标：满足全部硬约束后，优先可选类别覆盖，再按 value 偏好（若有）/预录 RRF 效用/价格/件数/SKU 字典序。预录排名不是实际向量输出；当前 Mall 执行仍仅关键词召回，quiet 等属性未评分。
- 窄 Beam 可能在所有类别均在窗口时漏解；窗口截断/展开上限也可能漏解。no_feasible_bundle 不是全库无解。外部降价不能使初次窗口之外的 SKU 自动获得最终核验资格；outside-window-not-revived 明确保留这一损失。
- 选择耗时仅包围 Select（包含该调用内部准备），不含 fixture/分类目录构造、穷举或报告评估。执行耗时包围 Executor.Run，包含内存适配器与核验；不含构造/穷举/评估。nearest-rank、小样本、未控制机器负载，不能外推生产 P95。
- 展开数是 Beam 评估尝试（含不可行尝试），不是 CPU 指令。贪心未插桩，展开数为 null 而非零；内存分配用独立 Benchmark -benchmem 测量，不在逐例质量报告中用全进程内存差冒充。
- 读取/核验是内存接口调用（失败也计数），不是网络 RPC。超时/取消故障为错误码注入，不是耗尽真实期限或证明取消传播；真实环境的鉴权、期限与故障联调仍待验收。
- 所有数据均是开发期已知合成回归样例，无独立 holdout，不修改旧 products/cases/review/retrieval 标签或旧基线。真实 Mall、Embedding、模型均 not_run，模型调用 0，Token/费用 null。
