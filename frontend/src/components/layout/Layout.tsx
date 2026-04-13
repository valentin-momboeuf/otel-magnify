import { Link, Outlet, useLocation } from 'react-router-dom'
import { useStore } from '../../store'

const navItems = [
  { path: '/', label: 'Dashboard' },
  { path: '/agents', label: 'Agents' },
  { path: '/configs', label: 'Configs' },
  { path: '/alerts', label: 'Alerts' },
]

export default function Layout() {
  const location = useLocation()
  const alertCount = useStore((s) => s.alerts.length)

  return (
    <div style={{ display: 'flex', minHeight: '100vh' }}>
      <nav style={{ width: 220, background: '#1a1a2e', color: '#fff', padding: '1rem' }}>
        <h2 style={{ fontSize: '1.2rem', marginBottom: '2rem' }}>otel-magnify</h2>
        <ul style={{ listStyle: 'none', padding: 0 }}>
          {navItems.map((item) => (
            <li key={item.path} style={{ marginBottom: '0.5rem' }}>
              <Link
                to={item.path}
                style={{
                  color: location.pathname === item.path ? '#4fc3f7' : '#ccc',
                  textDecoration: 'none',
                }}
              >
                {item.label}
                {item.label === 'Alerts' && alertCount > 0 && (
                  <span style={{ marginLeft: 8, background: '#e53935', borderRadius: 8, padding: '2px 6px', fontSize: '0.75rem' }}>
                    {alertCount}
                  </span>
                )}
              </Link>
            </li>
          ))}
        </ul>
      </nav>
      <main style={{ flex: 1, padding: '1.5rem', background: '#f5f5f5' }}>
        <Outlet />
      </main>
    </div>
  )
}
