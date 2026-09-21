# Agent 检索排序回放对比

合成排序输入、待人工复核；不是真实语义效果、Mall 搜索性能或推荐套装质量报告。默认线上策略不变。

- 数据版本：retrieval_rankings_v1；SHA-256：`c261ee9dbceca64a9e82d0f93804332c1cc2cfea192f0d152f6f54716e2ed05f`。
- 协议：bounded-rank-replay-v1；SHA-256：`af23263b08d10271649bafa0d5ab335512f68de5062b1711b91dc527f2ab4d94`。
- 代码标记：0d4c823+M3.3b-worktree（调用者提供，不是签名认证）。
- TopK=4，初始窗口=8，最大窗口=16，RRF 常量=60，混合单次检索期限=3000 ms。
- 环境：linux / amd64，go1.26.8，GOMAXPROCS=2；用例串行、混合支路并发，无预热、无远程缓存。

## 策略与共同边界

- keyword_only / vector_only：生产 HybridProductProvider 的单路消融，另一支路固定为空且不计源读取；复用相同去重、过滤、扩召回，不是新增线上模式。
- hybrid_rrf：生产混合 Provider，两路读取固定排序前缀。
- vector_first：生产 RAGProductProvider，保留原有窗口下限 8、按 MaxItems 放大及错误/空结果回退；关键词回退读取整个 MaxK 有界快照，不模拟真实 Mall 分页。
- 所有策略均经过候选归一化、单 SKU 预算过滤，再取同一 TopK；原始返回量和每次读取窗口逐例记录，不声称四种策略读取成本相同。
- 只评估检索候选，不运行 Planner、最终核验、组合选择或真实模型；MaxItems 不是候选池大小上限。

## 质量用例汇总

| 策略 | 质量/故障用例 | Recall@K | Precision@K | nDCG@K | 无相关样本的非空率 | 质量源读取（词/向量） | 扩召回 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| keyword_only | 10/8 | 12/20（60.00%） | 12/40（30.00%） | 0.551415（8 例） | 1/2（50.00%） | 15/0 | 5 |
| vector_only | 10/8 | 16/20（80.00%） | 16/40（40.00%） | 0.722701（8 例） | 1/2（50.00%） | 0/15 | 5 |
| vector_first | 10/8 | 12/20（60.00%） | 12/40（30.00%） | 0.597701（8 例） | 1/2（50.00%） | 1/10 | 0 |
| hybrid_rrf | 10/8 | 16/20（80.00%） | 16/40（40.00%） | 0.738905（8 例） | 1/2（50.00%） | 12/12 | 2 |

## 机制门禁与本机回放开销

| 策略 | 预期终态 | 违规用例 | 全部源读取（词/向量） | 回退/成功降级 | 质量 P50/P95（ms） |
| --- | --- | --- | --- | --- | --- |
| keyword_only | 18/18（100.00%） | 0 | 25/0 | 0/0 | 0.2760/17.4475 |
| vector_only | 18/18（100.00%） | 0 | 0/24 | 0/0 | 0.0365/1.4348 |
| vector_first | 18/18（100.00%） | 0 | 5/18 | 5/2 | 0.0046/3.0314 |
| hybrid_rrf | 18/18（100.00%） | 0 | 22/21 | 0/3 | 0.0266/0.3395 |

机制门禁：true。只要求预期终态、独立目录事实及调用/窗口边界；不以 RRF 必须获胜作为通过条件。

## RRF 相对对照的逐例 nDCG 变化

| 对照 | 改善 | 退化 | 持平 |
| --- | --- | --- | --- |
| keyword_only | consensus_match, complementary_lanes, vector_alias, consensus_noise, duplicate_ranks |  | keyword_exact, filtered_prefix, missing_relevant |
| vector_only | complementary_lanes, keyword_exact, duplicate_ranks | vector_alias, consensus_noise | consensus_match, filtered_prefix, missing_relevant |
| vector_first | complementary_lanes, keyword_exact, filtered_prefix, duplicate_ranks | vector_alias, consensus_noise | consensus_match, missing_relevant |

## 逐例结果

| 用例 | 类型 | 策略 | 实际/预期 | TopK SKU | 相关命中 | 读取窗口 | 违规 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| consensus_match | quality | keyword_only | ok/ok | k1, n1, k2, n2 | 2/2 | keyword:8 |  |
| complementary_lanes | quality | keyword_only | ok/ok | k1, n1, k2, n2 | 2/4 | keyword:8 |  |
| vector_alias | quality | keyword_only | ok/ok | n1, n2, n3, n4 | 0/2 | keyword:8 |  |
| keyword_exact | quality | keyword_only | ok/ok | k4, n1, n2, n3 | 1/1 | keyword:8 |  |
| consensus_noise | quality | keyword_only | ok/ok | n1, n2, n3, k1 | 1/3 | keyword:8 |  |
| filtered_prefix | quality | keyword_only | ok/ok | k1, k2, k3, k4 | 4/4 | keyword:8, keyword:16 |  |
| duplicate_ranks | quality | keyword_only | ok/ok | k1, n1, k2 | 2/3 | keyword:8, keyword:16 |  |
| missing_relevant | quality | keyword_only | ok/ok | n1, n2 | 0/1 | keyword:8, keyword:16 |  |
| negative_with_noise | quality | keyword_only | ok/ok | n1, n2 | 0/0 | keyword:8, keyword:16 |  |
| empty_slice | quality | keyword_only | ok/ok |  | 0/0 | keyword:8, keyword:16 |  |
| vector_unavailable | fault | keyword_only | ok/ok | k1, k2, k3, k4 | 0/0 | keyword:8 |  |
| keyword_unavailable | fault | keyword_only | unavailable/unavailable |  | 0/0 | keyword:8 |  |
| both_unavailable | fault | keyword_only | unavailable/unavailable |  | 0/0 | keyword:8 |  |
| vector_denied | fault | keyword_only | ok/ok | k1, k2, k3, k4 | 0/0 | keyword:8 |  |
| keyword_denied | fault | keyword_only | unauthenticated/unauthenticated |  | 0/0 | keyword:8 |  |
| empty_keyword_vector_failed | fault | keyword_only | ok/ok |  | 0/0 | keyword:8, keyword:16 |  |
| wider_keyword_failed | fault | keyword_only | unavailable/unavailable |  | 0/0 | keyword:8, keyword:16 |  |
| vector_deadline_code | fault | keyword_only | ok/ok | k1, k2, k3, k4 | 0/0 | keyword:8 |  |
| consensus_match | quality | vector_only | ok/ok | k1, k2, n3, n4 | 2/2 | vector:8 |  |
| complementary_lanes | quality | vector_only | ok/ok | k3, n4, k4, n5 | 2/4 | vector:8 |  |
| vector_alias | quality | vector_only | ok/ok | k1, k2, n1, n2 | 2/2 | vector:8 |  |
| keyword_exact | quality | vector_only | ok/ok | n4, n5, n6, k4 | 1/1 | vector:8 |  |
| consensus_noise | quality | vector_only | ok/ok | k1, k2, k3, n1 | 3/3 | vector:8 |  |
| filtered_prefix | quality | vector_only | ok/ok | k1, k2, k3, k4 | 4/4 | vector:8, vector:16 |  |
| duplicate_ranks | quality | vector_only | ok/ok | k2, k3, n2 | 2/3 | vector:8, vector:16 |  |
| missing_relevant | quality | vector_only | ok/ok | n3, n4 | 0/1 | vector:8, vector:16 |  |
| negative_with_noise | quality | vector_only | ok/ok | n3, n4 | 0/0 | vector:8, vector:16 |  |
| empty_slice | quality | vector_only | ok/ok |  | 0/0 | vector:8, vector:16 |  |
| vector_unavailable | fault | vector_only | unavailable/unavailable |  | 0/0 | vector:8 |  |
| keyword_unavailable | fault | vector_only | ok/ok | k1, k2, k3, k4 | 0/0 | vector:8 |  |
| both_unavailable | fault | vector_only | unavailable/unavailable |  | 0/0 | vector:8 |  |
| vector_denied | fault | vector_only | permission_denied/permission_denied |  | 0/0 | vector:8 |  |
| keyword_denied | fault | vector_only | ok/ok | k1, k2, k3, k4 | 0/0 | vector:8 |  |
| empty_keyword_vector_failed | fault | vector_only | unavailable/unavailable |  | 0/0 | vector:8 |  |
| wider_keyword_failed | fault | vector_only | ok/ok |  | 0/0 | vector:8, vector:16 |  |
| vector_deadline_code | fault | vector_only | unavailable/unavailable |  | 0/0 | vector:8 |  |
| consensus_match | quality | vector_first | ok/ok | k1, k2, n3, n4 | 2/2 | vector:8 |  |
| complementary_lanes | quality | vector_first | ok/ok | k3, n4, k4, n5 | 2/4 | vector:8 |  |
| vector_alias | quality | vector_first | ok/ok | k1, k2, n1, n2 | 2/2 | vector:8 |  |
| keyword_exact | quality | vector_first | ok/ok | n4, n5, n6, k4 | 1/1 | vector:8 |  |
| consensus_noise | quality | vector_first | ok/ok | k1, k2, k3, n1 | 3/3 | vector:8 |  |
| filtered_prefix | quality | vector_first | ok/ok |  | 0/4 | vector:8 |  |
| duplicate_ranks | quality | vector_first | ok/ok | k2, k3, n2 | 2/3 | vector:8 |  |
| missing_relevant | quality | vector_first | ok/ok | n3, n4 | 0/1 | vector:8 |  |
| negative_with_noise | quality | vector_first | ok/ok | n3, n4 | 0/0 | vector:8 |  |
| empty_slice | quality | vector_first | ok/ok |  | 0/0 | keyword:16, vector:8 |  |
| vector_unavailable | fault | vector_first | ok/ok | k1, k2, k3, k4 | 0/0 | keyword:16, vector:8 |  |
| keyword_unavailable | fault | vector_first | ok/ok | k1, k2, k3, k4 | 0/0 | vector:8 |  |
| both_unavailable | fault | vector_first | unavailable/unavailable |  | 0/0 | keyword:16, vector:8 |  |
| vector_denied | fault | vector_first | permission_denied/permission_denied |  | 0/0 | vector:8 |  |
| keyword_denied | fault | vector_first | ok/ok | k1, k2, k3, k4 | 0/0 | vector:8 |  |
| empty_keyword_vector_failed | fault | vector_first | ok/ok |  | 0/0 | keyword:16, vector:8 |  |
| wider_keyword_failed | fault | vector_first | unavailable/unavailable |  | 0/0 | keyword:16, vector:8 |  |
| vector_deadline_code | fault | vector_first | deadline_exceeded/deadline_exceeded |  | 0/0 | vector:8 |  |
| consensus_match | quality | hybrid_rrf | ok/ok | k1, k2, k3, n1 | 2/2 | keyword:8, vector:8 |  |
| complementary_lanes | quality | hybrid_rrf | ok/ok | k1, k3, n1, n4 | 2/4 | keyword:8, vector:8 |  |
| vector_alias | quality | hybrid_rrf | ok/ok | n1, n2, k1, k2 | 2/2 | keyword:8, vector:8 |  |
| keyword_exact | quality | hybrid_rrf | ok/ok | k4, n4, n1, n5 | 1/1 | keyword:8, vector:8 |  |
| consensus_noise | quality | hybrid_rrf | ok/ok | k1, n1, k2, n2 | 2/3 | keyword:8, vector:8 |  |
| filtered_prefix | quality | hybrid_rrf | ok/ok | k1, k2, k3, k4 | 4/4 | keyword:8, keyword:16, vector:8, vector:16 |  |
| duplicate_ranks | quality | hybrid_rrf | ok/ok | k2, k1, k3, n1 | 3/3 | keyword:8, vector:8 |  |
| missing_relevant | quality | hybrid_rrf | ok/ok | n1, n3, n2, n4 | 0/1 | keyword:8, vector:8 |  |
| negative_with_noise | quality | hybrid_rrf | ok/ok | n1, n3, n2, n4 | 0/0 | keyword:8, vector:8 |  |
| empty_slice | quality | hybrid_rrf | ok/ok |  | 0/0 | keyword:8, keyword:16, vector:8, vector:16 |  |
| vector_unavailable | fault | hybrid_rrf | ok/ok | k1, k2, k3, k4 | 0/0 | keyword:8, vector:8 |  |
| keyword_unavailable | fault | hybrid_rrf | ok/ok | k1, k2, k3, k4 | 0/0 | keyword:8, vector:8 |  |
| both_unavailable | fault | hybrid_rrf | unavailable/unavailable |  | 0/0 | keyword:8, vector:8 |  |
| vector_denied | fault | hybrid_rrf | permission_denied/permission_denied |  | 0/0 | keyword:8, vector:8 |  |
| keyword_denied | fault | hybrid_rrf | unauthenticated/unauthenticated |  | 0/0 | keyword:8, vector:8 |  |
| empty_keyword_vector_failed | fault | hybrid_rrf | unavailable/unavailable |  | 0/0 | keyword:8, keyword:16, vector:8 |  |
| wider_keyword_failed | fault | hybrid_rrf | unavailable/unavailable |  | 0/0 | keyword:8, keyword:16, vector:8, vector:16 |  |
| vector_deadline_code | fault | hybrid_rrf | ok/ok | k1, k2, k3, k4 | 0/0 | keyword:8, vector:8 |  |

## 指标和未运行项

- 质量与故障注入用例分开统计；质量用例即使失败也不移出分母。Recall 为命中总数/相关标注总数；Precision 为命中总数/(K×质量用例数)，不足 K 不缩小分母。
- nDCG 使用二值相关性：DCG=Σ hit/log2(rank+1)，除以理想前 min(K,相关数) 名的 DCG；无相关标注不进入 nDCG/Recall 分母，另报负例非空率。违规结果的质量分归零，不从聚合中删除。
- 配对变化仅统计有相关标注的质量用例；共识噪声、漏召回和融合退化不删样本、不改标签。没有质量改善阈值或默认策略切换。
- 源读取是内存适配器调用，包含失败尝试；不是 Mall RPC 次数、Embedding 请求、Token 或费用。向量分数只是名次倒数占位值，不是模型输出。
- P50/P95 采用 nearest-rank，仅测质量用例的本机 Provider 调用，不含加载/校验/评估；小样本、固定执行顺序且未控制机器负载，不能证明生产延迟。超时故障仅注入错误码，不等待真实期限。
- 真实 Mall、Embedding、模型、最终库存核验均为 not_run；Token/费用为 null。全部样本是公开的已知回归输入，无独立 holdout 或人工验收；报告来源标记不构成认证。
