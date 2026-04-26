import { useNavigate } from 'react-router-dom'

interface Props {
  label: string
  value: number
  link?: string
  delta?: number
}

export default function StatCard({ label, value, link, delta }: Props) {
  const navigate = useNavigate()
  const clickable = Boolean(link)
  const onClick = clickable ? () => navigate(link!) : undefined

  return (
    <div
      className={`stat-card${clickable ? ' stat-card-link' : ''}`}
      onClick={onClick}
      role={clickable ? 'button' : undefined}
      tabIndex={clickable ? 0 : undefined}
    >
      <div className="stat-value">{value}</div>
      <div className="stat-label">{label}</div>
      {delta !== undefined && <DeltaChip value={delta} />}
    </div>
  )
}

function DeltaChip({ value }: { value: number }) {
  const sign = value > 0 ? '+' : ''
  const tone = value > 0 ? 'positive' : value < 0 ? 'negative' : 'neutral'
  return (
    <span className={`delta-chip delta-${tone}`}>
      {sign}
      {value}
    </span>
  )
}
