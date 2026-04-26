import { useTranslation } from 'react-i18next'
import type { Workload } from '../../types'

interface Props {
  workloads: Workload[]
}

const MAX_ROWS = 5

export default function DeployedVersionsPanel({ workloads }: Props) {
  const { t } = useTranslation()

  const counts = new Map<string, number>()
  for (const w of workloads) {
    const key = w.version || '—'
    counts.set(key, (counts.get(key) ?? 0) + 1)
  }
  const all = [...counts.entries()]
    .map(([version, count]) => ({ version, count }))
    .sort((a, b) => b.count - a.count)

  const rows = all.slice(0, MAX_ROWS)
  const extraCount = all.slice(MAX_ROWS).reduce((acc, r) => acc + r.count, 0)
  const max = Math.max(1, ...rows.map((r) => r.count))

  return (
    <section className="panel">
      <header className="panel-head">
        <h2 className="panel-title">{t('dashboard.panel.deployed_versions')}</h2>
      </header>

      {rows.length === 0 ? (
        <div className="versions-empty">{t('dashboard.versions.empty')}</div>
      ) : (
        <div className="versions-list">
          {rows.map((r) => (
            <div key={r.version} className="versions-row">
              <span className="versions-label">{r.version}</span>
              <span className="versions-bar">
                <span
                  className="versions-bar-fill"
                  style={{ width: `${(r.count / max) * 100}%` }}
                />
              </span>
              <span className="versions-count">{r.count}</span>
            </div>
          ))}
          {extraCount > 0 && (
            <div className="versions-empty">
              {t('dashboard.versions.others', { count: extraCount })}
            </div>
          )}
        </div>
      )}
    </section>
  )
}
