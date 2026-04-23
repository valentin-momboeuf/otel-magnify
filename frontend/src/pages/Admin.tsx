import { Navigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useStore } from '../store'
import { hasPerm } from '../lib/perm'

export default function Admin() {
  const { t } = useTranslation()
  const me = useStore((s) => s.me)

  if (!me) return null
  if (!hasPerm(me.groups, 'users:manage')) return <Navigate to="/" replace />

  return (
    <div className="page-profile">
      <h2>{t('admin.title')}</h2>
      <section className="profile-section">
        <p style={{ color: 'var(--text-muted)' }}>{t('admin.placeholder')}</p>
      </section>
    </div>
  )
}
