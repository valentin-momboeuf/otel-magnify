import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { pushesAPI } from '../../api/client'

const CHART_HEIGHT = 120
const FLOOR = 2

export default function PushActivityPanel() {
  const { t } = useTranslation()
  const { data } = useQuery({
    queryKey: ['push-activity', '7d'],
    queryFn: () => pushesAPI.activity('7d'),
    staleTime: 60_000,
  })

  const points = data ?? []
  const total = points.reduce((acc, p) => acc + p.count, 0)
  const max = Math.max(1, ...points.map((p) => p.count))

  return (
    <section className="panel">
      <header className="panel-head">
        <h2 className="panel-title">{t('dashboard.panel.push_activity')}</h2>
        <span className="panel-hint">{total}</span>
      </header>

      <svg className="push-chart" viewBox="0 0 280 140" preserveAspectRatio="none" aria-label={t('dashboard.panel.push_activity')}>
        {points.map((p, i) => {
          const barW = 28
          const gap = (280 - barW * 7) / 6
          const x = i * (barW + gap)
          const h = p.count === 0 ? FLOOR : Math.max(FLOOR, (p.count / max) * CHART_HEIGHT)
          const y = CHART_HEIGHT - h
          const isLast = i === points.length - 1
          return (
            <rect
              key={p.day}
              x={x}
              y={y}
              width={barW}
              height={h}
              rx={2}
              className={`push-chart-bar${isLast ? ' push-chart-bar-last' : ''}`}
            >
              <title>{`${p.day}: ${p.count}`}</title>
            </rect>
          )
        })}
      </svg>

      <div className="push-chart-labels">
        {points.map((p) => (
          <span key={p.day}>{dayInitial(p.day)}</span>
        ))}
      </div>

      {total === 0 && <div className="push-chart-empty">{t('dashboard.push.empty')}</div>}
    </section>
  )
}

function dayInitial(day: string): string {
  const d = new Date(day + 'T00:00:00Z')
  return ['S', 'M', 'T', 'W', 'T', 'F', 'S'][d.getUTCDay()]
}
