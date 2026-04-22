import { Link } from 'react-router-dom'
import type { Workload } from '../../types'
import StatusBadge from './StatusBadge'
import { isSupervised } from '../../lib/workloadCapabilities'
import { useStore } from '../../store'

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

export default function WorkloadCard({ workload }: { workload: Workload }) {
  const connectedCount = useStore((s) => s.connectedInstanceCounts[workload.id])

  return (
    <Link to={`/workloads/${workload.id}`} className="workload-card">
      <div className="workload-card-main">
        <TypeIcon type={workload.type} />
        <div>
          <div className="workload-name">{workload.display_name || workload.id}</div>
          <div className="workload-meta">
            <span className={`workload-type workload-type-${workload.type}`}>{workload.type}</span>
            {isSupervised(workload) && (
              <span
                className="workload-supervised-pill"
                title="Managed by an OpAMP Supervisor — accepts remote config pushes"
              >
                supervised
              </span>
            )}
            &nbsp;&nbsp;v{workload.version}
          </div>
        </div>
      </div>
      <div className="workload-card-side">
        {typeof connectedCount === 'number' && (
          <span className="instance-count-badge" title="Connected instances">
            {connectedCount} {connectedCount === 1 ? 'instance' : 'instances'}
          </span>
        )}
        <StatusBadge status={workload.status} />
      </div>
    </Link>
  )
}
