import { useParams, Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { agentsAPI } from '../api/client'
import StatusBadge from '../components/agents/StatusBadge'

export default function AgentDetail() {
  const { id } = useParams<{ id: string }>()
  const { data: agent, isLoading } = useQuery({
    queryKey: ['agent', id],
    queryFn: () => agentsAPI.get(id!),
    enabled: !!id,
  })

  if (isLoading) return <div className="loading">Loading agent...</div>
  if (!agent)   return <div className="error-text">Agent not found.</div>

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">{agent.display_name || agent.id}</h1>
        <Link to="/agents" style={{ fontFamily: 'var(--mono)', fontSize: '0.7rem', color: 'var(--muted)', textDecoration: 'none' }}>
          ← Agents
        </Link>
      </div>

      {/* Key metrics grid */}
      <div className="detail-grid">
        <div className="detail-cell">
          <div className="detail-cell-label">Type</div>
          <div className="detail-cell-value">{agent.type}</div>
        </div>
        <div className="detail-cell">
          <div className="detail-cell-label">Version</div>
          <div className="detail-cell-value">v{agent.version}</div>
        </div>
        <div className="detail-cell">
          <div className="detail-cell-label">Status</div>
          <div className="detail-cell-value">
            <StatusBadge status={agent.status} />
          </div>
        </div>
        <div className="detail-cell">
          <div className="detail-cell-label">Last seen</div>
          <div className="detail-cell-value" style={{ fontSize: '0.78rem' }}>
            {new Date(agent.last_seen_at).toLocaleString()}
          </div>
        </div>
        {agent.active_config_id && (
          <div className="detail-cell">
            <div className="detail-cell-label">Active config</div>
            <div className="detail-cell-value">
              <code>{agent.active_config_id.substring(0, 12)}...</code>
            </div>
          </div>
        )}
      </div>

      {/* Labels */}
      {Object.keys(agent.labels).length > 0 && (
        <>
          <p className="section-title">Labels</p>
          <div style={{ display: 'flex', gap: '0.4rem', flexWrap: 'wrap' }}>
            {Object.entries(agent.labels).map(([k, v]) => (
              <span key={k} className="label-chip">
                <span className="label-chip-key">{k}</span>
                <span className="label-chip-eq">=</span>
                <span className="label-chip-val">{v}</span>
              </span>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
