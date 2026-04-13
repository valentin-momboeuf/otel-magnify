import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip, Legend } from 'recharts'
import { agentsAPI, alertsAPI } from '../api/client'
import { useStore } from '../store'
import StatusBadge from '../components/agents/StatusBadge'

export default function Dashboard() {
  const { data: agents } = useQuery({ queryKey: ['agents'], queryFn: agentsAPI.list })
  const { data: alerts } = useQuery({ queryKey: ['alerts'], queryFn: () => alertsAPI.list(false) })

  const store = useStore()

  useEffect(() => {
    if (agents) store.setAgents(agents)
  }, [agents])

  useEffect(() => {
    if (alerts) store.setAlerts(alerts)
  }, [alerts])

  const connected = agents?.filter((a) => a.status === 'connected').length ?? 0
  const degraded  = agents?.filter((a) => a.status === 'degraded').length ?? 0
  const total     = agents?.length ?? 0

  const statusData = [
    { name: 'Connected',    value: connected,                                                          color: '#34d399' },
    { name: 'Disconnected', value: (agents?.filter(a => a.status === 'disconnected').length ?? 0),    color: '#6b7280' },
    { name: 'Degraded',     value: degraded,                                                           color: '#fbbf24' },
  ].filter(d => d.value > 0)

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Dashboard</h1>
      </div>

      <div className="stat-grid">
        <StatCard label="Total Agents"  value={total}              />
        <StatCard label="Connected"     value={connected}          />
        <StatCard label="Degraded"      value={degraded}           />
        <StatCard label="Active Alerts" value={alerts?.length ?? 0} />
      </div>

      {statusData.length > 0 && (
        <div style={{ display: 'flex', gap: '2rem', marginBottom: '2rem', alignItems: 'center' }}>
          <div style={{ width: 220, height: 220 }}>
            <ResponsiveContainer>
              <PieChart>
                <Pie data={statusData} dataKey="value" nameKey="name" cx="50%" cy="50%" innerRadius={55} outerRadius={85} paddingAngle={3} strokeWidth={0}>
                  {statusData.map((entry, i) => (
                    <Cell key={i} fill={entry.color} />
                  ))}
                </Pie>
                <Tooltip contentStyle={{ background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: '6px', fontFamily: 'var(--mono)', fontSize: '0.78rem' }} />
                <Legend wrapperStyle={{ fontFamily: 'var(--sans)', fontSize: '0.75rem' }} />
              </PieChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      <p className="section-title">Recent Alerts</p>

      {alerts && alerts.length > 0 ? (
        <table className="data-table">
          <thead>
            <tr>
              <th>Agent</th>
              <th>Rule</th>
              <th>Severity</th>
              <th>Message</th>
              <th>Fired at</th>
            </tr>
          </thead>
          <tbody>
            {alerts.slice(0, 5).map((a) => (
              <tr key={a.id}>
                <td><code>{a.agent_id}</code></td>
                <td><code>{a.rule}</code></td>
                <td><StatusBadge status={a.severity} /></td>
                <td>{a.message}</td>
                <td style={{ whiteSpace: 'nowrap', fontFamily: 'var(--mono)', fontSize: '0.75rem', color: 'var(--muted)' }}>
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

function StatCard({ label, value }: { label: string; value: number }) {
  return (
    <div className="stat-card">
      <div className="stat-value">{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  )
}
