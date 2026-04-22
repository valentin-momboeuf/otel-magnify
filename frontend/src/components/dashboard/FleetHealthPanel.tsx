import { useTranslation } from 'react-i18next'
import type { Workload } from '../../types'

interface Props {
  workloads: Workload[]
}

const RADIUS = 36
const STROKE = 10
const CIRC = 2 * Math.PI * RADIUS

export default function FleetHealthPanel({ workloads }: Props) {
  const { t } = useTranslation()
  const connected    = workloads.filter((w) => w.status === 'connected').length
  const degraded     = workloads.filter((w) => w.status === 'degraded').length
  const disconnected = workloads.filter((w) => w.status === 'disconnected').length
  const total        = connected + degraded + disconnected

  const arcs = total > 0
    ? [
        { color: 'var(--green)', share: connected / total },
        { color: 'var(--amber)', share: degraded / total },
        { color: 'var(--red)',   share: disconnected / total },
      ]
    : []

  let offset = 0
  const segments = arcs.map((arc) => {
    const length = arc.share * CIRC
    const seg = { ...arc, length, offset }
    offset += length
    return seg
  })

  return (
    <section className="panel">
      <header className="panel-head">
        <h2 className="panel-title">{t('dashboard.panel.fleet_health')}</h2>
        <span className="panel-hint">{total}</span>
      </header>

      <div className="fleet-health-body">
        <svg className="fleet-donut" viewBox="0 0 100 100" aria-label={t('dashboard.panel.fleet_health')}>
          <circle cx={50} cy={50} r={RADIUS} className="fleet-donut-track" strokeWidth={STROKE} />
          <g transform="rotate(-90 50 50)">
            {total === 0 ? (
              <circle cx={50} cy={50} r={RADIUS} className="fleet-donut-track" strokeWidth={STROKE} />
            ) : (
              segments.map((seg, i) => (
                <circle
                  key={i}
                  cx={50}
                  cy={50}
                  r={RADIUS}
                  className="fleet-donut-arc"
                  stroke={seg.color}
                  strokeWidth={STROKE}
                  strokeDasharray={`${seg.length} ${CIRC}`}
                  strokeDashoffset={-seg.offset}
                />
              ))
            )}
          </g>
          <text x={50} y={52} textAnchor="middle" className="fleet-donut-label">{total}</text>
        </svg>

        <div className="fleet-breakdown">
          <FleetRow dotClass="fleet-dot-connected"    label={t('dashboard.fleet.connected')}    value={connected} />
          <FleetRow dotClass="fleet-dot-degraded"     label={t('dashboard.fleet.degraded')}     value={degraded} />
          <FleetRow dotClass="fleet-dot-disconnected" label={t('dashboard.fleet.disconnected')} value={disconnected} />
        </div>
      </div>
    </section>
  )
}

function FleetRow({ dotClass, label, value }: { dotClass: string; label: string; value: number }) {
  return (
    <div className="fleet-breakdown-row">
      <span className="fleet-breakdown-label">
        <span className={`fleet-dot ${dotClass}`} />
        {label}
      </span>
      <span className="fleet-breakdown-value">{value}</span>
    </div>
  )
}
