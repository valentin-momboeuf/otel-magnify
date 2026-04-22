import { useTranslation } from 'react-i18next'
import type { Alert } from '../../types'
import StatusBadge from '../workloads/StatusBadge'

interface Props {
  alerts: Alert[]
}

export default function RecentAlertsPanel({ alerts }: Props) {
  const { t } = useTranslation()
  const rows = alerts.slice(0, 4)

  return (
    <section className="panel">
      <header className="panel-head">
        <h2 className="panel-title">{t('dashboard.panel.recent_alerts')}</h2>
        <span className="panel-hint">{alerts.length}</span>
      </header>

      {rows.length === 0 ? (
        <div className="empty-state">{t('dashboard.alerts.empty')}</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Workload</th>
              <th>Rule</th>
              <th>Severity</th>
              <th>Message</th>
              <th>Fired at</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((a) => (
              <tr key={a.id}>
                <td><code>{a.workload_id}</code></td>
                <td><code>{a.rule}</code></td>
                <td><StatusBadge status={a.severity} /></td>
                <td>{a.message}</td>
                <td className="table-timestamp">{new Date(a.fired_at).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}
