import { Link } from 'react-router-dom'
import type { Agent } from '../../types'
import StatusBadge from './StatusBadge'

export default function AgentCard({ agent }: { agent: Agent }) {
  return (
    <Link to={`/agents/${agent.id}`} style={{ textDecoration: 'none', color: 'inherit' }}>
      <div style={{
        background: '#fff', padding: '1rem', borderRadius: 8,
        boxShadow: '0 1px 3px rgba(0,0,0,0.1)', marginBottom: '0.5rem',
        display: 'flex', justifyContent: 'space-between', alignItems: 'center',
      }}>
        <div>
          <div style={{ fontWeight: 600 }}>{agent.display_name || agent.id}</div>
          <div style={{ fontSize: '0.85rem', color: '#666' }}>
            {agent.type} &middot; v{agent.version}
          </div>
        </div>
        <StatusBadge status={agent.status} />
      </div>
    </Link>
  )
}
