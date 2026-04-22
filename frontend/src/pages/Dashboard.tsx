import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { workloadsAPI, alertsAPI } from '../api/client'
import { useStore } from '../store'
import StatusBadge from '../components/workloads/StatusBadge'
import { isSupervised } from '../lib/workloadCapabilities'

export default function Dashboard() {
  const { data: workloads } = useQuery({ queryKey: ['workloads'], queryFn: () => workloadsAPI.list() })
  const { data: alerts } = useQuery({ queryKey: ['alerts'], queryFn: () => alertsAPI.list(false) })

  const store = useStore()

  useEffect(() => {
    if (workloads) store.setWorkloads(workloads)
  }, [workloads])

  useEffect(() => {
    if (alerts) store.setAlerts(alerts)
  }, [alerts])

  const connected  = workloads?.filter((w) => w.status === 'connected').length ?? 0
  const degraded   = workloads?.filter((w) => w.status === 'degraded').length ?? 0
  const collectors = workloads?.filter((w) => w.type === 'collector').length ?? 0
  const sdks       = workloads?.filter((w) => w.type === 'sdk').length ?? 0
  const supervised = workloads?.filter(isSupervised).length ?? 0

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Dashboard</h1>
      </div>

      <div className="stat-grid">
        <StatCard label="Collectors"    value={collectors}         link="/inventory?type=collector" />
        <StatCard label="SDK Workloads" value={sdks}               link="/inventory?type=sdk" />
        <StatCard label="Supervised"    value={supervised}         link="/inventory?control=supervised" />
        <StatCard label="Connected"     value={connected}          />
        <StatCard label="Degraded"      value={degraded}           />
        <StatCard label="Active Alerts" value={alerts?.length ?? 0} />
      </div>

      <p className="section-title">Recent Alerts</p>

      {alerts && alerts.length > 0 ? (
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
            {alerts.slice(0, 5).map((a) => (
              <tr key={a.id}>
                <td><code>{a.workload_id}</code></td>
                <td><code>{a.rule}</code></td>
                <td><StatusBadge status={a.severity} /></td>
                <td>{a.message}</td>
                <td className="table-timestamp">
                  {new Date(a.fired_at).toLocaleString()}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <div className="empty-state">No active alerts</div>
      )}
    </div>
  )
}

function StatCard({ label, value, link }: { label: string; value: number; link?: string }) {
  const navigate = useNavigate()
  return (
    <div
      className={`stat-card${link ? ' stat-card-link' : ''}`}
      onClick={link ? () => navigate(link) : undefined}
    >
      <div className="stat-value">{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  )
}
