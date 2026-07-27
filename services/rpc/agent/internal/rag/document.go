package rag

import (
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/schema"
)

// 文档 MetaData 的约定 key。
const (
	// metaCandidate 存放 CandidateMetadata，由 Loader 写入、Indexer 序列化入库、Retriever 反序列化还原。
	metaCandidate = "candidate"
	// metaContentHash 存放内容指纹，由 Pipeline 计算、Indexer 入库。
	metaContentHash = "content_hash"
	// metaProductId 存放所属商品 ID。
	metaProductId = "product_id"
)

// CandidateMetadata 是向量表 metadata 列的结构，承载检索后还原候选商品所需的业务快照。
// 价格/库存/销量只存在这里，不进 embedding 文本——价格波动不应触发重嵌入。
type CandidateMetadata struct {
	ProductId  string   `json:"product_id"`
	Name       string   `json:"name"`
	Category   string   `json:"category"`
	Brand      string   `json:"brand,omitempty"`
	PriceCents int64    `json:"price_cents"`
	Stock      int64    `json:"stock"`
	Sold       int64    `json:"sold"`
	Source     string   `json:"source"`
	Tags       []string `json:"tags,omitempty"`
}

// NewCandidateDocument 构造携带业务快照的商品文档，Loader 与 Retriever 统一走这里，
// 保证 MetaData 结构一致（测试构造检索结果时也复用）。
func NewCandidateDocument(id, content string, meta CandidateMetadata) *schema.Document {
	return &schema.Document{
		ID:      id,
		Content: content,
		MetaData: map[string]any{
			metaCandidate: meta,
			metaProductId: meta.ProductId,
		},
	}
}

// CandidateFromDocument 从检索返回的 Document 中取出业务快照。
func CandidateFromDocument(doc *schema.Document) (CandidateMetadata, bool) {
	if doc == nil || doc.MetaData == nil {
		return CandidateMetadata{}, false
	}
	meta, ok := doc.MetaData[metaCandidate].(CandidateMetadata)
	return meta, ok
}

// candidateJSON 把业务快照序列化为 metadata 列的 JSON。
func candidateJSON(meta CandidateMetadata) (string, error) {
	data, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("rag: marshal candidate metadata: %w", err)
	}
	return string(data), nil
}

// candidateFromJSON 从 metadata 列还原业务快照。
func candidateFromJSON(raw string) (CandidateMetadata, error) {
	var meta CandidateMetadata
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return CandidateMetadata{}, fmt.Errorf("rag: unmarshal candidate metadata: %w", err)
	}
	return meta, nil
}
