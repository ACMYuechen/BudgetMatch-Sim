import { formatPrice } from '@/utils/format'

interface PriceDisplayProps {
  cents: number
  className?: string
}

export function PriceDisplay({ cents, className }: PriceDisplayProps) {
  return <span className={`text-red-500 font-semibold ${className}`}>{formatPrice(cents)}</span>
}
