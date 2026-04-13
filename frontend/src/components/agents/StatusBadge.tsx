interface Props {
  status: string
}

// Maps a status string to its CSS modifier class
function badgeClass(status: string): string {
  const map: Record<string, string> = {
    connected:    'badge-connected',
    disconnected: 'badge-disconnected',
    degraded:     'badge-degraded',
    warning:      'badge-warning',
    critical:     'badge-critical',
  }
  return map[status] ?? 'badge-disconnected'
}

export default function StatusBadge({ status }: Props) {
  return (
    <span className={`badge ${badgeClass(status)}`}>
      {status}
    </span>
  )
}
