# Agent 离线规则基线

合成快照与标注，待人工复核；这是代码行为基线，不是真实模型/商城效果或生产性能结论。

- 快照：synthetic-products-v1；策略：offline-keyword-rule-v1；代码标记：ad91f2b+M2.2-worktree（调用者提供）。
- 快照 SHA-256：`3feaf027662f36321442e6ec35f3255da98eed34ca917322c9113e25ee5e833f`。
- 用例 SHA-256：`c0615d42d182d93ab653007365d56e2ff6237191bd5da0c834980515214b2d3a`。
- 环境：linux / amd64，go1.26.8，12 逻辑 CPU；并发 1，TopK=10，划分=all。
- 每个用例新建内存会话；进程复用、无显式预热、无外部缓存；历史准备和重放不计入本轮推荐延迟。

## 对照运行状态

| 基线 | 状态 | 范围 |
| --- | --- | --- |
| keyword_rule | executed | fixed snapshot adapter + production Planner/Service/BundleSelector; no Mall RPC |
| vector_rule | not_run | real embedding and retrieval comparison not configured |
| vector_react | not_run | real embedding/model evaluation requires explicit data and cost authorization |

## 汇总

| 指标 | 结果 |
| --- | --- |
| 完成结果硬约束违规率 | 0/50（0.00%） |
| 预期终态/错误类别命中 | 64/64（100.00%） |
| 可满足任务成功率 | 25/40（62.50%） |
| 需求覆盖率（含不可满足需求） | 34/61（55.74%） |
| Recall@K（micro） | 92/92（100.00%） |
| 完成结果事实一致性 | 50/50（100.00%） |
| 规则兜底率（含显式故障注入） | 2/64（3.12%） |
| 成功轮次重放通过率 | 50/50（100.00%） |
| 持久化轮次数符合预期 | 64/64（100.00%） |

总请求 64，完成 50，违规请求 0，无相关 SKU 标注 22；商品查询 52 次，故障替身调用 2 次，模型调用 0 次。Token/费用、真实向量召回、解释文本评审均未测量。

本地端到端 P50=0.1524 ms，P95=0.3373 ms（nearest-rank，小样本且无外部依赖，仅作本机参考）。安全/终态/幂等门禁：true；该门禁不代表推荐质量达标。

## 固定划分

| 划分 | 用例数 | 可满足成功 | 覆盖率 | Recall@K |
| --- | --- | --- | --- | --- |
| dev | 31 | 12/21（57.14%） | 16/31（51.61%） | 54/54（100.00%） |
| holdout | 33 | 13/19（68.42%） | 18/30（60.00%） | 38/38（100.00%） |

## 场景

| 场景 | 用例数 | 可满足成功 | 终态命中 |
| --- | --- | --- | --- |
| availability | 4 | 2/2（100.00%） | 4/4（100.00%） |
| budget_insufficient | 6 | 未适用（0 个有效样本） | 6/6（100.00%） |
| conflict_input | 12 | 未适用（0 个有效样本） | 12/12（100.00%） |
| dependency_fault | 4 | 2/2（100.00%） | 4/4（100.00%） |
| multi_category | 8 | 1/8（12.50%） | 8/8（100.00%） |
| multi_turn | 8 | 7/7（100.00%） | 8/8（100.00%） |
| noise | 4 | 0/4（0.00%） | 4/4（100.00%） |
| numeric_boundary | 2 | 1/1（100.00%） | 2/2（100.00%） |
| prompt_injection | 4 | 3/4（75.00%） | 4/4（100.00%） |
| single | 12 | 9/12（75.00%） | 12/12（100.00%） |

## 未通过任务或门禁的样本

| 用例 | 划分 | 终态 | 覆盖 | 选择 SKU | 硬约束问题 |
| --- | --- | --- | --- | --- | --- |
| single_mouse_100 | dev | completed | 0/1 | mouse_basic |  |
| single_keyboard_100 | dev | completed | 0/1 | noise_brush |  |
| single_keyboard_400 | dev | completed | 0/1 | noise_brush |  |
| multi_keyboard_mouse | dev | completed | 1/2 | noise_brush, mouse_basic |  |
| multi_monitor_keyboard | holdout | completed | 1/2 | noise_brush, kb_basic |  |
| multi_phone_audio | holdout | completed | 1/2 | earbuds_travel, headset_office |  |
| multi_tablet_audio | holdout | completed | 1/2 | earbuds_travel, headset_office |  |
| multi_study_light | dev | completed | 0/3 | noise_brush, pen_pack, notebook_grid |  |
| multi_english | dev | completed | 1/2 | noise_brush, mouse_basic |  |
| multi_stationery_audio | holdout | completed | 1/2 | pen_pack, notebook_grid |  |
| noise_keyboard_accessory | dev | completed | 0/1 | noise_brush |  |
| noise_monitor_sticker | holdout | completed | 0/1 | noise_sticker |  |
| noise_office_audio | dev | completed | 0/1 | mouse_basic |  |
| noise_study_tablet | holdout | completed | 0/1 | pen_pack |  |
| injection_product_text | dev | completed | 0/1 | noise_brush |  |

## 口径与限制

- 可满足成功必须非空、满足全部必需需求、仅含允许 SKU，且通过独立快照/预算/件数校验。
- Recall@K 以当前轮过滤后的 TopK 为分子来源，跨用例求命中数/相关 SKU 数；无相关 SKU 不进分母。
- 需求覆盖率包含标注不可满足的需求，因此它的上限未必为 100%。
- 错误/拒绝必须不新增完成轮次；成功重放必须同结果、零新增轮次/商品查询/故障替身调用。
- dev 用于未来调参；holdout 按用例族固定隔离，不凭本次结果移动划分或修改答案。
- 故障 primary 是确定性 Agent 替身，不是 Fake Model；真实向量、ReAct、模型注入抵抗和 Token/费用仍未运行。
- 查询、商品描述不包含真实个人数据；注入用例仅验证此规则路径的边界，不能外推到真实 LLM。


## 同数据版本对比

cb5512a+M2.1-worktree → ad91f2b+M2.2-worktree（调用者提供的代码标记）。

| 指标 | 之前 | 当前 |
| --- | --- | --- |
| 预期终态 | 62/64（96.88%） | 64/64（100.00%） |
| 可满足任务 | 24/40（60.00%） | 25/40（62.50%） |
| 需求覆盖 | 33/61（54.10%） | 34/61（55.74%） |
| Recall@K | 91/92（98.91%） | 92/92（100.00%） |
| 硬约束违规 | 0/48（0.00%） | 0/50（0.00%） |

终态修复：history_budget_down, history_unsatisfiable_after_cut；终态退化：无。
任务修复：history_budget_down；任务退化：无。

保留原始标注和划分；修复已知 holdout 反例后，该集合属于已知回归集，不再声称是盲测。环境负载未控制，延迟差异不作为性能提升证据。
