import { NavLink, Outlet } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { useStore } from '../../store'
import '../../styles/sidebar.css'

function IconDashboard() {
  return (
    <svg className="nav-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
      <rect x="1.5" y="1.5" width="5" height="5" rx="1" />
      <rect x="9.5" y="1.5" width="5" height="5" rx="1" />
      <rect x="1.5" y="9.5" width="5" height="5" rx="1" />
      <rect x="9.5" y="9.5" width="5" height="5" rx="1" />
    </svg>
  )
}

function IconInventory() {
  return (
    <svg className="nav-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="8" cy="5" r="2.5" />
      <path d="M2.5 15c0-3 2.5-5 5.5-5s5.5 2 5.5 5" />
    </svg>
  )
}

function IconConfigs() {
  return (
    <svg className="nav-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round">
      <path d="M3 4h10M3 8h7M3 12h5" />
    </svg>
  )
}

function IconAlerts() {
  return (
    <svg className="nav-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round">
      <path d="M8 2L2 13h12L8 2z" />
      <path d="M8 6.5v3" />
      <circle cx="8" cy="11" r="0.5" fill="currentColor" stroke="none" />
    </svg>
  )
}

const navItems = [
  { path: '/',          key: 'dashboard', Icon: IconDashboard, end: true  },
  { path: '/inventory', key: 'inventory', Icon: IconInventory, end: false },
  { path: '/configs',   key: 'configs',   Icon: IconConfigs,   end: false },
  { path: '/alerts',    key: 'alerts',    Icon: IconAlerts,    end: false },
] as const

export default function Layout() {
  const { t } = useTranslation()
  const alertCount = useStore((s) => s.alerts.length)

  return (
    <div className="app-layout">
      <aside className="sidebar">
        <div className="sidebar-logo">
          <div className="sidebar-logo-name">
            otel<span>-magnify</span>
          </div>
          <div className="sidebar-logo-sub">{t('sidebar.subtitle')}</div>
          <span className="sidebar-signal-dot" aria-hidden />
          <span className="sidebar-signal-bar" aria-hidden />
        </div>

        <nav>
          <div className="sidebar-section-label">{t('sidebar.section.fleet')}</div>
          <ul className="sidebar-nav">
            {navItems.map(({ path, key, Icon, end }) => (
              <li key={path} className="sidebar-nav-item">
                <NavLink to={path} end={end}>
                  <Icon />
                  <span>{t(`sidebar.nav.${key}`)}</span>
                  {key === 'alerts' && alertCount > 0 && (
                    <span className="sidebar-badge">{alertCount}</span>
                  )}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>

        <div className="sidebar-footer">
          <span className="sidebar-footer-dot" aria-hidden />
          {t('sidebar.footer.live')} · v{__APP_VERSION__}
        </div>
      </aside>

      <main className="main-content">
        <Outlet />
      </main>
    </div>
  )
}
