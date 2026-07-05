package productservicelogic

import (
	"budgetmatch-sim/services/rpc/mall/model/products"
	"budgetmatch-sim/services/rpc/mall/model/product_skus"
	"budgetmatch-sim/services/rpc/mall/pb"
)

func productToPb(p *products.Products) *pb.Product {
	if p == nil {
		return nil
	}
	return &pb.Product{
		Id:           p.Id,
		UserId:       p.UserId,
		Name:         p.Name,
		Content:      p.Content,
		Image:        p.Image,
		Providor:     p.Providor,
		Status:       p.Status,
		AgentComment: p.AgentComment,
		CreatedAt:    p.CreatedAt.Format(timeLayout),
		UpdatedAt:    p.UpdatedAt.Format(timeLayout),
	}
}

func skuToPb(s *product_skus.ProductSkus) *pb.Sku {
	if s == nil {
		return nil
	}
	return &pb.Sku{
		Id:           s.Id,
		ProductId:    s.ProductId,
		Name:         s.Name,
		Specs:        s.Specs,
		Price:        s.Price,
		Stock:        int64(s.Stock),
		Sold:         int64(s.Sold),
		Status:       int32(s.Status),
		AgentComment: s.AgentComment,
		CreatedAt:    s.CreatedAt.Format(timeLayout),
		UpdatedAt:    s.UpdatedAt.Format(timeLayout),
	}
}

const timeLayout = "2006-01-02T15:04:05Z07:00"
