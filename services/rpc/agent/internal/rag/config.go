// Package rag 提供基于 pgvector 的商品语义检索能力。
//
// 组件全部实现 Eino 官方抽象并接入 callbacks 观测：
//   - MallProductLoader (document.Loader)：分页拉取 mall 上架商品，每 SKU 一个 Document；
//   - Indexer (indexer.Indexer)：批量 embedding 后写入 product_vectors 表；
//   - Retriever (retriever.Retriever)：余弦相似度检索；
//   - Pipeline + Syncer：完整加载并校验、准备向量，再事务发布及清理；支持超时与取消。
//
// 商品文档短且结构化，Pipeline 保留 document.Transformer 位但默认为 nil（无需切分），
// 与 Eino Loader/Transformer/Indexer/Retriever 四件套抽象保持对齐。
package rag

// 默认配置值。
const (
	defaultTopK         = 10
	defaultSyncInterval = 600
	defaultSyncPageSize = 100
	defaultSyncTimeout  = 300
	defaultStopTimeout  = 5
	maxSyncTimeout      = 3600
	maxStopTimeout      = 30
)

// Config 是 RAG 行为配置。
type Config struct {
	TopK                   int     `json:"topK,optional"`                   // TopK 检索返回条数上限，默认 10
	ScoreThreshold         float64 `json:"scoreThreshold,optional"`         // ScoreThreshold 余弦相似度下限 [0,1]，默认 0 不过滤
	SyncIntervalSeconds    int     `json:"syncIntervalSeconds,optional"`    // SyncIntervalSeconds 定时同步间隔秒数，默认 600；负数表示仅启动时同步一次
	SyncPageSize           int32   `json:"syncPageSize,optional"`           // SyncPageSize 离线同步分页大小，默认 100
	SyncTimeoutSeconds     int     `json:"syncTimeoutSeconds,optional"`     // 单轮扫描/嵌入/发布的共同期限，默认 300 秒，上限 3600 秒
	SyncStopTimeoutSeconds int     `json:"syncStopTimeoutSeconds,optional"` // Stop 取消后最多等待，默认 5 秒，上限 30 秒
	VerifySku              bool    `json:"verifySku,optional"`              // VerifySku 检索后是否经 mall GetSku 实时校验价格库存
}

// Normalize 返回归一化后的配置。
func (c Config) Normalize() Config {
	if c.TopK <= 0 {
		c.TopK = defaultTopK
	}
	if c.SyncIntervalSeconds == 0 {
		c.SyncIntervalSeconds = defaultSyncInterval
	}
	if c.SyncPageSize <= 0 {
		c.SyncPageSize = defaultSyncPageSize
	}
	if c.SyncTimeoutSeconds <= 0 {
		c.SyncTimeoutSeconds = defaultSyncTimeout
	}
	c.SyncTimeoutSeconds = min(c.SyncTimeoutSeconds, maxSyncTimeout)
	if c.SyncStopTimeoutSeconds <= 0 {
		c.SyncStopTimeoutSeconds = defaultStopTimeout
	}
	c.SyncStopTimeoutSeconds = min(c.SyncStopTimeoutSeconds, maxStopTimeout)
	return c
}
