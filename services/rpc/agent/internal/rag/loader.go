package rag

import (
	"context"
	"fmt"
	"io"
	"strings"

	"budgetmatch-sim/services/rpc/mall/indexcontract"
	"budgetmatch-sim/services/rpc/mall/pb"

	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components"
	"github.com/cloudwego/eino/components/document"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

const (
	// SourceMallProducts 是商品全量加载的约定 Source URI。
	SourceMallProducts = "mall://products"

	// 与 Mall 索引 RPC 的单页上限保持一致。
	maxIndexPageSize = indexcontract.MaxPageSize
	// detailLimit 限制进入 embedding 文本的商品简介长度。
	detailLimit = 500
)

// mallCatalog 只暴露后台专用只读索引，不借用用户商品 API。
type mallCatalog interface {
	ScanProductIndex(ctx context.Context, in *pb.ScanProductIndexReq, opts ...grpc.CallOption) (pb.ProductIndexService_ScanProductIndexClient, error)
}

// MallProductLoader 实现 eino document.Loader：分页拉取 mall 全量上架商品与 SKU，
// 每个上架 SKU 产出一个 Document（Content 为语义文本，业务快照进 MetaData）。
// 各页来自同一个只读数据库快照；只接受连续帧、完成标志和正常 EOF。
// 超时/超限/失败返回 nil 而非部分文档，也不回退到旧版非快照分页 RPC。
type MallProductLoader struct {
	client   mallCatalog
	pageSize int32
}

// 确保 MallProductLoader 实现 document.Loader。
var _ document.Loader = (*MallProductLoader)(nil)

// NewMallProductLoader 创建 mall 商品加载器。
func NewMallProductLoader(client mallCatalog, pageSize int32) *MallProductLoader {
	if pageSize <= 0 {
		pageSize = defaultSyncPageSize
	}
	if pageSize > maxIndexPageSize {
		pageSize = maxIndexPageSize
	}
	return &MallProductLoader{client: client, pageSize: pageSize}
}

// Load 全量加载上架商品文档。
func (l *MallProductLoader) Load(ctx context.Context, src document.Source, opts ...document.LoaderOption) (docs []*schema.Document, err error) {
	ctx = callbacks.EnsureRunInfo(ctx, l.GetType(), components.ComponentOfLoader)
	ctx = callbacks.OnStart(ctx, &document.LoaderCallbackInput{Source: src})
	defer func() {
		if err != nil {
			callbacks.OnError(ctx, err)
		}
	}()

	if l.client == nil {
		return nil, fmt.Errorf("rag: product index client is required")
	}
	ctx, cancel := context.WithTimeout(ctx, indexcontract.MaxDuration)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stream, err := l.client.ScanProductIndex(ctx, &pb.ScanProductIndexReq{PageSize: l.pageSize},
		grpc.MaxCallRecvMsgSize(indexcontract.MaxPageBytes))
	if err != nil {
		return nil, fmt.Errorf("rag: open product snapshot: %w", err)
	}
	if stream == nil {
		return nil, fmt.Errorf("rag: missing product snapshot stream")
	}
	var budget indexcontract.Budget
	cursor, snapshotID := "", ""
	sequence := uint32(0)
	complete, shortPage := false, false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		resp, recvErr := stream.Recv()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if recvErr == io.EOF {
			if !complete {
				return nil, fmt.Errorf("rag: product snapshot ended without completion")
			}
			break
		}
		if recvErr != nil {
			return nil, fmt.Errorf("rag: read product snapshot: %w", recvErr)
		}
		if resp == nil || complete || resp.Sequence != sequence+1 || len(resp.List) > int(l.pageSize) ||
			!budget.Add(len(resp.List), proto.Size(resp)) {
			return nil, fmt.Errorf("rag: invalid or oversized product snapshot frame")
		}
		if snapshotID == "" {
			if len(resp.SnapshotId) != 36 || uuid.Validate(resp.SnapshotId) != nil {
				return nil, fmt.Errorf("rag: invalid product snapshot identity")
			}
			snapshotID = resp.SnapshotId
		}
		if resp.SnapshotId != snapshotID {
			return nil, fmt.Errorf("rag: product snapshot identity changed")
		}
		sequence = resp.Sequence
		if resp.Complete {
			if len(resp.List) != 0 || resp.LastSkuId != "" || resp.TotalItems != uint64(len(docs)) {
				return nil, fmt.Errorf("rag: invalid product snapshot completion")
			}
			complete = true
			continue // A terminal marker alone must never authorize pruning.
		}
		if len(resp.List) == 0 || resp.TotalItems != 0 || shortPage {
			return nil, fmt.Errorf("rag: invalid product snapshot data page")
		}
		shortPage = len(resp.List) < int(l.pageSize)
		last := cursor
		for _, entry := range resp.List {
			if entry == nil || !validIndexID(entry.SkuId) || !validIndexID(entry.ProductId) || entry.SkuId <= last {
				return nil, fmt.Errorf("rag: invalid or non-progressing product index entry")
			}
			last = entry.SkuId
			docs = append(docs, buildSkuDocument(
				&pb.Product{Id: entry.ProductId, Name: entry.ProductName, Content: entry.ProductContent, Providor: entry.Provider},
				&pb.Sku{Id: entry.SkuId, Name: entry.SkuName, Specs: entry.Specs, Price: entry.Price, Stock: entry.Stock, Sold: entry.Sold},
			))
		}
		if resp.LastSkuId != last {
			return nil, fmt.Errorf("rag: inconsistent product snapshot cursor")
		}
		cursor = last
	}

	callbacks.OnEnd(ctx, &document.LoaderCallbackOutput{Source: src, Docs: docs})
	return docs, nil
}

// buildSkuDocument 组装单个 SKU 的文档：语义文本进 Content，业务快照进 MetaData。
func buildSkuDocument(product *pb.Product, sku *pb.Sku) *schema.Document {
	name := joinNonEmpty(" ", product.Name, sku.Name)
	content := buildContent(product, sku, name)
	meta := CandidateMetadata{
		ProductId:  product.Id,
		Name:       name,
		Brand:      product.Providor,
		PriceCents: sku.Price,
		Stock:      sku.Stock,
		Sold:       sku.Sold,
		Source:     "mall",
		Tags:       loaderTags(product.Providor, sku.Specs),
	}
	return NewCandidateDocument(sku.Id, content, meta)
}

// buildContent 组装参与 embedding 的语义文本。刻意不含价格/库存/销量：
// 这些高频波动字段进 MetaData，避免每次价格变动都触发重嵌入。
func buildContent(product *pb.Product, sku *pb.Sku, name string) string {
	var b strings.Builder
	b.WriteString("商品: ")
	b.WriteString(name)
	if providor := strings.TrimSpace(product.Providor); providor != "" {
		b.WriteString("\n供应商: ")
		b.WriteString(providor)
	}
	if specs := strings.TrimSpace(sku.Specs); specs != "" {
		b.WriteString("\n规格: ")
		b.WriteString(specs)
	}
	if content := strings.TrimSpace(product.Content); content != "" {
		b.WriteString("\n简介: ")
		b.WriteString(truncateRunes(content, detailLimit))
	}
	return b.String()
}

// GetType 返回组件类型标识。
func (l *MallProductLoader) GetType() string {
	return "MallProductLoader"
}

// IsCallbacksEnabled 声明组件自带 callbacks 切面。
func (l *MallProductLoader) IsCallbacksEnabled() bool {
	return true
}

// joinNonEmpty 拼接非空片段并去重相邻重复。
func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == part {
			continue
		}
		out = append(out, part)
	}
	return strings.Join(out, sep)
}

// loaderTags 组装候选标签：供应商 + 规格原文。
func loaderTags(providor, specs string) []string {
	var tags []string
	if providor = strings.TrimSpace(providor); providor != "" {
		tags = append(tags, providor)
	}
	if specs = strings.TrimSpace(specs); specs != "" {
		tags = append(tags, specs)
	}
	return tags
}

// truncateRunes 按字符截断文本，避免把多字节字符截成半个。
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
