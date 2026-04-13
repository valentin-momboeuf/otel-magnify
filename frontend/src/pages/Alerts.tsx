import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { alertsAPI } from '../api/client'
import StatusBadge from '../components/agents/StatusBadge'

export default function Alerts() {
  const queryClient = useQueryClient()
  const { data: alerts, isLoading } = useQuery({
    queryKey: ['alerts'],
    queryFn: () => alertsAPI.list(false),
  })

  const resolveMutation = useMutation({
    mutationFn: (id: string) => alertsAPI.resolve(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['alerts'] }),
  })

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Alerts</h1>
        {(alerts?.length ?? 0) > 0 && (
          <span style={{ fontFamily: 'var(--mono)', fontSize: '0.7rem', color: 'var(--danger)' }}>
            {alerts?.length} active
          </span>
        )}
      </div>

      {isLoading ? (
        <div className="loading">Loading alerts...</div>
      ) : (alerts ?? []).length === 0 ? (
        <div className="empty-state">No active alerts</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Agent</th>
              <th>Rule</th>
              <th>Severity</th>
              <th>Message</th>
              <th>Fired at</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {(alerts ?? []).map((a) => (
              <tr key={a.id}>
                <td><code>{a.agent_id}</code></td>
                <td><code>{a.rule}</code></td>
                <td><StatusBadge status={a.severity} /></td>
                <td style={{ maxWidth: 320 }}>{a.message}</td>
                <td style={{ fontFamily: 'var(--mono)', fontSize: '0.75rem', color: 'var(--muted)', whiteSpace: 'nowrap' }}>
                  {new Date(a.fired_at).toLocaleString()}
                </td>
                <td>
                  <button
                    className="btn-resolve"
                    onClick={() => resolveMutation.mutate(a.id)}
                    disabled={resolveMutation.isPending}
                  >
                    resolve
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
