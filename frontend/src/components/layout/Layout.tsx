import { Link, Outlet, useLocation } from 'react-router-dom'
import { useStore } from '../../store'

// SVG icons as inline components — keeps the bundle small, no icon dep needed
function IconDashboard() {
  return (
    <svg className="nav-icon" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.5">
      <rect x="1" y="1" width="5" height="5" rx="0.5" />
      <rect x="8" y="1" width="5" height="5" rx="0.5" />
      <rect x="1" y="8" width="5" height="5" rx="0.5" />
      <rect x="8" y="8" width="5" height="5" rx="0.5" />
    </svg>
  )
}

function IconAgents() {
  return (
    <svg className="nav-icon" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.5">
      <circle cx="7" cy="4" r="2.5" />
      <path d="M1 13c0-3.3 2.7-5 6-5s6 1.7 6 5" />
    </svg>
  )
}

function IconConfigs() {
  return (
    <svg className="nav-icon" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.5">
      <path d="M2 3h10M2 7h7M2 11h5" strokeLinecap="round" />
    </svg>
  )
}

function IconAlerts() {
  return (
    <svg className="nav-icon" viewBox="0 0 14 14" fill="none" stroke="currentColor" strokeWidth="1.5">
      <path d="M7 1L1 12h12L7 1z" />
      <path d="M7 5.5v3M7 10v.5" strokeLinecap="round" />
    </svg>
  )
}

const navItems = [
  { path: '/',        label: 'Dashboard', Icon: IconDashboard },
  { path: '/agents',  label: 'Agents',    Icon: IconAgents    },
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
            // Exact match for root, prefix match for sub-routes
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
