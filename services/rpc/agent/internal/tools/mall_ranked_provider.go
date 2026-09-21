package tools

import (
	"context"

	"budgetmatch-sim/services/rpc/agent/internal/rag"
	"budgetmatch-sim/services/rpc/mall/candidatecontract"
	"budgetmatch-sim/services/rpc/mall/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	maxRankedMallCalls     = 12
	maxRankedTerms         = 4
	rankedProductPageSize  = 10
	maxRankedPages         = 2
	maxRankedResponseBytes = 256 << 10
)

// SearchRankedProducts preserves Mall's search/page order (not BM25 scoring).
// Both loop counts and RPC payloads are bounded, independently of the return cap.
// It is separate from SearchProducts so the default keyword baseline is unchanged.
func (p *MallProductProvider) SearchRankedProducts(ctx context.Context, req SearchProductsReq, limit int) ([]ProductCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > rag.MaxHybridWindow {
		return nil, status.Error(codes.InvalidArgument, "invalid keyword window")
	}
	if p == nil || p.client == nil {
		return nil, status.Error(codes.Unavailable, "keyword provider unavailable")
	}
	terms := searchTerms(req)
	terms = terms[:min(len(terms), maxRankedTerms)]
	var out []ProductCandidate
	calls := 0
	seenProducts := make(map[string]bool)
	for _, term := range terms {
		for page := int32(1); page <= maxRankedPages; page++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if calls == maxRankedMallCalls {
				return out, nil
			}
			calls++
			resp, err := p.client.ListProducts(ctx, &pb.ListProductsReq{Keyword: term, Status: mallStatusOnShelf,
				Page: page, PageSize: rankedProductPageSize}, grpc.MaxCallRecvMsgSize(maxRankedResponseBytes))
			if err != nil {
				return nil, err
			}
			if resp == nil || len(resp.List) > rankedProductPageSize || proto.Size(resp) > maxRankedResponseBytes {
				return nil, status.Error(codes.Unavailable, "invalid keyword page")
			}
			for _, product := range resp.List {
				if product == nil || !candidatecontract.ValidID(product.Id) {
					return nil, status.Error(codes.Unavailable, "invalid keyword product")
				}
				if seenProducts[product.Id] || product.Status != mallStatusOnShelf {
					continue
				}
				seenProducts[product.Id] = true
				// Keep page size fixed across a product's pages; shrinking it with
				// the remaining capacity would change offset and reread old rows.
				size := int32(min(limit, 32))
				for skuPage := int32(1); skuPage <= maxRankedPages; skuPage++ {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					if calls == maxRankedMallCalls {
						return out, nil
					}
					calls++
					skus, err := p.client.ListSkusByProduct(ctx, &pb.ListSkusByProductReq{ProductId: product.Id,
						Status: mallStatusOnShelf, Page: skuPage, PageSize: size}, grpc.MaxCallRecvMsgSize(maxRankedResponseBytes))
					if err != nil {
						return nil, err
					}
					if skus == nil || len(skus.List) > int(size) || proto.Size(skus) > maxRankedResponseBytes {
						return nil, status.Error(codes.Unavailable, "invalid keyword SKU page")
					}
					for _, sku := range skus.List {
						if sku == nil || !candidatecontract.ValidID(sku.Id) || sku.ProductId != product.Id {
							return nil, status.Error(codes.Unavailable, "invalid keyword SKU identity")
						}
						candidate := candidateFromSku(product, sku)
						if sku.Status != mallStatusOnShelf {
							candidate.Stock = 0
						}
						out = append(out, candidate)
						if len(out) == limit {
							if err := ctx.Err(); err != nil {
								return nil, err
							}
							return out, nil
						}
					}
					if len(skus.List) < int(size) || int64(skuPage)*int64(size) >= skus.Total {
						break
					}
				}
			}
			if len(resp.List) < rankedProductPageSize || int64(page)*rankedProductPageSize >= resp.Total {
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
