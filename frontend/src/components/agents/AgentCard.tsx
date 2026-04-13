import { Link } from 'react-router-dom'
import type { Agent } from '../../types'
import StatusBadge from './StatusBadge'

function TypeIcon({ type }: { type: string }) {
  if (type === 'collector') {
    return (
      <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="var(--gold)" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round">
        <path d="M2 4h12M2 8h12M2 12h12" />
        <circle cx="5" cy="4" r="1" fill="var(--gold)" stroke="none" />
        <circle cx="8" cy="8" r="1" fill="var(--gold)" stroke="none" />
        <circle cx="11" cy="12" r="1" fill="var(--gold)" stroke="none" />
      </svg>
    )
  }
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="var(--blue)" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round">
      <rect x="3" y="2" width="10" height="12" rx="2" />
      <path d="M6 6h4M6 9h2" />
    </svg>
  )
}

export default function AgentCard({ agent }: { agent: Agent }) {
  return (
    <Link to={`/inventory/${agent.id}`} className="agent-card">
      <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
        <TypeIcon type={agent.type} />
        <div>
          <div className="agent-name">{agent.display_name || agent.id}</div>
          <div className="agent-meta">
            <span className={`agent-type agent-type-${agent.type}`}>{agent.type}</span>
            &nbsp;&nbsp;v{agent.version}
          </div>
        </div>
      </div>
      <StatusBadge status={agent.status} />
    </Link>
  )
}
