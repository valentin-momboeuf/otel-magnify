import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { authAPI } from '../api/client'

export default function Login() {
  const navigate  = useNavigate()
  const [email,    setEmail]    = useState('')
  const [password, setPassword] = useState('')
  const [error,    setError]    = useState('')
  const [loading,  setLoading]  = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const { token } = await authAPI.login(email, password)
      localStorage.setItem('token', token)
      navigate('/')
    } catch {
      setError('Invalid credentials')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-bg">
      <form onSubmit={handleSubmit} className="login-form">
        <div className="login-logo">
          otel<span>-magnify</span>
        </div>
        <div className="login-sub">OpAMP Control Plane</div>

        {error && (
          <div className="error-text">{error}</div>
        )}

        <div className="field">
          <label className="field-label">Email</label>
          <input
            type="email"
            className="field-input"
            placeholder="ops@example.com"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            autoComplete="email"
          />
        </div>

        <div className="field">
          <label className="field-label">Password</label>
          <input
            type="password"
            className="field-input"
            placeholder="••••••••"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
          />
        </div>

        <button
          type="submit"
          className="btn btn-primary"
          disabled={loading}
          style={{ width: '100%', justifyContent: 'center', padding: '0.6rem', marginTop: '0.5rem' }}
        >
          {loading ? 'Authenticating...' : 'Sign in'}
        </button>
      </form>
    </div>
  )
}
