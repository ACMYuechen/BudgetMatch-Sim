import { Pagination as AntPagination } from 'antd'

interface PaginationProps {
  page: number
  pageSize: number
  total: number
  onChange: (page: number, pageSize: number) => void
  pageSizeOptions?: number[]
}

export function Pagination({ page, pageSize, total, onChange, pageSizeOptions = [10, 20, 50] }: PaginationProps) {
  if (total <= 0) return null

  return (
    <div className="list-pagination">
      <AntPagination
        current={page}
        pageSize={pageSize}
        total={total}
        showSizeChanger
        responsive
        pageSizeOptions={pageSizeOptions}
        showTotal={(t) => `共 ${t} 条`}
        onChange={onChange}
      />
    </div>
  )
}
