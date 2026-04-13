const colors: Record<string, string> = {
  connected: '#4caf50',
  disconnected: '#9e9e9e',
  degraded: '#ff9800',
}

export default function StatusBadge({ status }: { status: string }) {
  return (
    <span
      style={{
        display: 'inline-block',
        padding: '2px 8px',
        borderRadius: 4,
        background: colors[status] ?? '#9e9e9e',
        color: '#fff',
        fontSize: '0.8rem',
        fontWeight: 600,
      }}
    >
      {status}
    </span>
  )
}
