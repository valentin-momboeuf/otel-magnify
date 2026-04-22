import { Link, Outlet, useLocation } from 'react-router-dom'
import { useStore } from '../../store'

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
  { path: '/',        label: 'Dashboard', Icon: IconDashboard },
  { path: '/inventory', label: 'Inventory', Icon: IconInventory    },
  { path: '/configs', label: 'Configs',   Icon: IconConfigs   },
  { path: '/alerts',  label: 'Alerts',    Icon: IconAlerts    },
]

export default function Layout() {
  const location = useLocation()
  const alertCount = useStore((s) => s.alerts.length)

  return (
    <div className="app-layout">
      <nav className="sidebar">
        <div className="sidebar-logo">
          <div className="sidebar-logo-name">
            otel<span>-magnify</span>
          </div>
          <div className="sidebar-logo-sub">OpAMP Control Plane</div>
          <div className="sidebar-signal" />
        </div>

        <ul className="sidebar-nav">
          {navItems.map(({ path, label, Icon }) => {
            const isActive = path === '/'
              ? location.pathname === '/'
              : location.pathname.startsWith(path)
            return (
              <li key={path} className="sidebar-nav-item">
                <Link to={path} className={isActive ? 'active' : ''}>
                  <Icon />
                  {label}
                  {label === 'Alerts' && alertCount > 0 && (
                    <span className="sidebar-badge">{alertCount}</span>
                  )}
                </Link>
              </li>
            )
          })}
        </ul>

        <div className="sidebar-footer">
          <span className="sidebar-footer-dot" />
          LIVE
        </div>
      </nav>

      <main className="main-content">
        <Outlet />
      </main>
    </div>
  )
}
