import { Link } from 'react-router-dom'
import type { Agent } from '../../types'
import StatusBadge from './StatusBadge'

export default function AgentCard({ agent }: { agent: Agent }) {
  return (
    <Link to={`/agents/${agent.id}`} className="agent-card">
      <div>
        <div className="agent-name">{agent.display_name || agent.id}</div>
        <div className="agent-meta">
          {agent.type}&nbsp;&nbsp;v{agent.version}
        </div>
      </div>
      <StatusBadge status={agent.status} />
    </Link>
  )
}
