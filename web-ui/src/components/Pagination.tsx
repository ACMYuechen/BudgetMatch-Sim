import { Pagination as AntPagination } from 'antd'

interface PaginationProps {
  page: number
  pageSize: number
  total: number
  onChange: (page: number, pageSize: number) => void
}

export function Pagination({ page, pageSize, total, onChange }: PaginationProps) {
  if (total <= 0) return null

  return (
    <div className="flex justify-end mt-6">
      <AntPagination
        current={page}
        pageSize={pageSize}
        total={total}
        showSizeChanger
        showTotal={(t) => `共 ${t} 条`}
        onChange={onChange}
      />
    </div>
  )
}
