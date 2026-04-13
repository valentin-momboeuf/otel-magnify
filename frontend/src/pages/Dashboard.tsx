import { useQuery } from '@tanstack/react-query'
import { agentsAPI, alertsAPI } from '../api/client'
import { useStore } from '../store'
import StatusBadge from '../components/agents/StatusBadge'
import type React from 'react'

export default function Dashboard() {
  const { data: agents } = useQuery({ queryKey: ['agents'], queryFn: agentsAPI.list })
  const { data: alerts } = useQuery({ queryKey: ['alerts'], queryFn: () => alertsAPI.list(false) })

  const store = useStore()
  if (agents && agents !== store.agents) store.setAgents(agents)
  if (alerts && alerts !== store.alerts) store.setAlerts(alerts)

  const connected = agents?.filter((a) => a.status === 'connected').length ?? 0
  const total = agents?.length ?? 0

  return (
    <div>
      <h1>Dashboard</h1>
      <div style={{ display: 'flex', gap: '1rem', marginBottom: '2rem' }}>
        <StatCard label="Total Agents" value={total} />
        <StatCard label="Connected" value={connected} />
        <StatCard label="Active Alerts" value={alerts?.length ?? 0} />
      </div>
      <h2>Recent Alerts</h2>
      {alerts && alerts.length > 0 ? (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr>
              <th style={th}>Agent</th><th style={th}>Rule</th><th style={th}>Severity</th><th style={th}>Message</th>
            </tr>
          </thead>
          <tbody>
            {alerts.slice(0, 5).map((a) => (
              <tr key={a.id}>
                <td style={td}>{a.agent_id}</td>
                <td style={td}>{a.rule}</td>
                <td style={td}><StatusBadge status={a.severity} /></td>
                <td style={td}>{a.message}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <p>No active alerts.</p>
      )}
    </div>
  )
}

function StatCard({ label, value }: { label: string; value: number }) {
  return (
    <div style={{ background: '#fff', padding: '1rem 2rem', borderRadius: 8, boxShadow: '0 1px 3px rgba(0,0,0,0.1)' }}>
      <div style={{ fontSize: '2rem', fontWeight: 700 }}>{value}</div>
      <div style={{ color: '#666' }}>{label}</div>
    </div>
  )
}

const th: React.CSSProperties = { textAlign: 'left', padding: '8px', borderBottom: '2px solid #ddd' }
const td: React.CSSProperties = { padding: '8px', borderBottom: '1px solid #eee' }
