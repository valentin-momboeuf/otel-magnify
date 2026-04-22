import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { alertsAPI } from '../api/client'
import StatusBadge from '../components/workloads/StatusBadge'

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
          <span className="page-header-count page-header-count-danger">
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
              <th>Workload</th>
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
                <td>
                  <Link to={`/workloads/${a.workload_id}`}><code>{a.workload_id}</code></Link>
                </td>
                <td><code>{a.rule}</code></td>
                <td><StatusBadge status={a.severity} /></td>
                <td className="alert-message-cell">{a.message}</td>
                <td className="table-timestamp">
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
