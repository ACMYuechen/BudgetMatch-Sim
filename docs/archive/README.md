# 历史归档

[文档导航](../README.md) · [项目状态](../status.md)

这里保存原始方案、阶段日志和验收证据。文中的“当前”“已完成”“下一步”、实例运行状态和测试数量仅对应记录时刻；旧命令不是新的操作授权，也不是当前部署步骤。

## 资料索引

| 归档 | 内容 | 当前指南 |
| --- | --- | --- |
| [Agent 开发记录](agent-development.md) | M1–M6 设计、不变量、协议、故障矩阵、实测与执行日志 | [Agent](../agent.md) |
| [权限核对快照](access-control-2026-09.md) | HTTP / RPC 逐接口矩阵、源码依据与历史测试范围 | [权限与安全](../access-control.md) |
| [前端交付阶段](frontend-stages.md) | 四阶段任务拆分与当时的回归记录 | [前端](../frontend-roadmap.md) |
| [本地数据维护](local-data-2026-09.md) | Docker 卷复用、pgvector 扩展、测试库/角色删除和私密备份索引 | [本地数据源](../local-data.md) |
| [VPS 发布与切库](deployment-vps-2026-09.md) | 旧版本发布、新外部库切换及各自验收范围 | [VPS 运维](../deployment-vps.md) |
| [CI 原始需求](../requirements/ci.md) | 最初设计基线，不是现行工作流说明 | [CI/CD 总览](../cicd.md) |

## Agent 记录

| 查阅主题 | 位置 |
| --- | --- |
| 接口、记忆、工具和索引细节 | [第 2 节](agent-development.md#2-现有接口会话与长期记忆) |
| 初始问题与设计不变量 | [第 3 节](agent-development.md#3-优化前基线与问题定位)、[第 4 节](agent-development.md#4-目标架构与不变量) |
| M1 约束与基础安全 | [第 6 节](agent-development.md#6-m1业务约束与基础安全) |
| M2 评测与人工复核 | [第 7 节](agent-development.md#7-m2评测集与可重复基线) |
| M3 索引与召回 | [第 8 节](agent-development.md#8-m3真实-rag-链路与检索质量) |
| M4 需求组合执行 | [第 9 节](agent-development.md#9-m4需求驱动的受约束组合推荐) |
| M5 流式与预算 | [第 10 节](agent-development.md#10-m5端到端流式运行预算与可观测性) |
| M6 并发、故障、真实依赖联调 | [第 11 节](agent-development.md#11-m6并发故障与交付验证) |
| 契约与回退、历史收尾条件 | [第 12 节](agent-development.md#12-契约配置与回滚)、[第 13 节](agent-development.md#13-验证入口与执行清单) |
| 按日期查具体改动和测试 | [第 14 节](agent-development.md#14-执行记录) |

本地规则评测、真实 Flash 小样本、真实 RAG 合成商品与生产发布各有独立范围，不能相互替代。评测数据、失败基线、工作单与报告仍保留在[原目录](../../services/rpc/agent/testdata/eval/)，没有因归档而改写结果。

归档不收录 `.env`、完整 DSN、Token、数据库 dump 或服务器 Secret。记录中提到的私密备份路径只是索引，文件不随仓库分发；读取和恢复仍需核对授权与目标。
