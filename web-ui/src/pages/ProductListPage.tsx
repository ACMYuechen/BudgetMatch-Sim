import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Input, Row, Col, Spin, Empty } from 'antd'
import { SearchOutlined } from '@ant-design/icons'
import { getProductList } from '@/api/mall'
import { ProductCard } from '@/components/ProductCard'
import { Pagination } from '@/components/Pagination'
import type { Product } from '@/types/api'

export default function ProductListPage() {
  const navigate = useNavigate()
  const [products, setProducts] = useState<Product[]>([])
  const [loading, setLoading] = useState(false)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(12)
  const [total, setTotal] = useState(0)
  const [keyword, setKeyword] = useState('')

  const fetchProducts = async (p = page, ps = pageSize, kw = keyword) => {
    setLoading(true)
    try {
      const res = await getProductList({
        page: p,
        page_size: ps,
        keyword: kw || undefined,
        status: 1,
      })
      setProducts(res.list || [])
      setTotal(res.total || 0)
      setPage(res.page)
      setPageSize(res.page_size)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchProducts(1, pageSize, keyword)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keyword])

  const handlePageChange = (p: number, ps: number) => {
    fetchProducts(p, ps, keyword)
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">商品列表</h1>
        <Input
          placeholder="搜索商品"
          prefix={<SearchOutlined />}
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          onPressEnter={() => fetchProducts(1, pageSize, keyword)}
          className="w-80"
          allowClear
        />
      </div>

      {loading ? (
        <div className="flex justify-center py-20">
          <Spin size="large" />
        </div>
      ) : products.length === 0 ? (
        <Empty description="暂无商品" />
      ) : (
        <>
          <Row gutter={[24, 24]}>
            {products.map((product) => (
              <Col xs={24} sm={12} lg={8} xl={6} key={product.id}>
                <ProductCard
                  product={product}
                  onClick={() => navigate(`/products/${product.id}`)}
                />
              </Col>
            ))}
          </Row>
          <Pagination
            page={page}
            pageSize={pageSize}
            total={total}
            onChange={handlePageChange}
          />
        </>
      )}
    </div>
  )
}
