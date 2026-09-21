package mall_admin

import (
	"net/http/httptest"
	"testing"

	"budgetmatch-sim/cmd/admin/internal/types"

	"github.com/zeromicro/go-zero/rest/httpx"
)

func TestProductListAllowsOptionalKeyword(test *testing.T) {
	for _, testCase := range []struct {
		name    string
		query   string
		keyword string
	}{
		{name: "omitted"},
		{name: "empty", query: "?keyword="},
		{name: "search", query: "?keyword=keyboard", keyword: "keyboard"},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			request := httptest.NewRequest("GET", "/api/admin/mall/products"+testCase.query, nil)
			var parsed types.AdminProductListReq
			if err := httpx.Parse(request, &parsed); err != nil {
				test.Fatalf("parse product list: %v", err)
			}
			if parsed.Keyword != testCase.keyword {
				test.Fatalf("keyword = %q, want %q", parsed.Keyword, testCase.keyword)
			}
		})
	}
}
