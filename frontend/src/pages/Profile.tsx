import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation } from '@tanstack/react-query'
import { useStore } from '../store'
import { meAPI } from '../api/client'
import i18n from '../i18n'
import '../styles/profile.css'

type Theme = 'light' | 'dark' | 'system'
type Lang = 'en' | 'fr'

function PasswordSection() {
  const { t } = useTranslation()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [confirm, setConfirm] = useState('')
  const [message, setMessage] = useState<{ kind: 'err' | 'ok'; text: string } | null>(null)

  const mutation = useMutation({
    mutationFn: ({ current, next }: { current: string; next: string }) =>
      meAPI.changePassword(current, next),
    onSuccess: () => {
      setCurrent(''); setNext(''); setConfirm('')
      setMessage({ kind: 'ok', text: t('profile.security.success') })
    },
    onError: (err: { response?: { status?: number; data?: { error?: string } } }) => {
      const status = err.response?.status
      if (status === 401) setMessage({ kind: 'err', text: t('profile.security.err_current') })
      else if (status === 400) setMessage({ kind: 'err', text: err.response?.data?.error ?? t('profile.security.err_weak') })
      else setMessage({ kind: 'err', text: t('profile.security.err_generic') })
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setMessage(null)
    if (next !== confirm) {
      setMessage({ kind: 'err', text: t('profile.security.err_mismatch') })
      return
    }
    mutation.mutate({ current, next })
  }

  return (
    <section className="profile-section">
      <h3>{t('profile.security.title')}</h3>
      <form onSubmit={handleSubmit} className="profile-form">
        <div className="field">
          <label className="field-label" htmlFor="pw-current">{t('profile.security.current')}</label>
          <input id="pw-current" type="password" className="field-input"
                 value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" />
        </div>
        <div className="field">
          <label className="field-label" htmlFor="pw-new">{t('profile.security.new')}</label>
          <input id="pw-new" type="password" className="field-input"
                 value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" />
        </div>
        <div className="field">
          <label className="field-label" htmlFor="pw-confirm">{t('profile.security.confirm')}</label>
          <input id="pw-confirm" type="password" className="field-input"
                 value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="new-password" />
        </div>
        {message && (
          <div className={message.kind === 'err' ? 'error-text' : 'success-text'}>{message.text}</div>
        )}
        <button type="submit" className="btn btn-primary" disabled={mutation.isPending}>
          {mutation.isPending ? t('common.loading') : t('profile.security.submit')}
        </button>
      </form>
    </section>
  )
}

function PreferencesSection() {
  const { t } = useTranslation()
  const me = useStore((s) => s.me)
  const updateMyPreferences = useStore((s) => s.updateMyPreferences)

  const [theme, setTheme] = useState<Theme>((me?.preferences.theme ?? 'system') as Theme)
  const [language, setLanguage] = useState<Lang>((me?.preferences.language ?? 'en') as Lang)

  const mutation = useMutation({
    mutationFn: (next: { theme: Theme; language: Lang }) => meAPI.updatePreferences(next),
    onSuccess: (saved) => {
      updateMyPreferences(saved)
      i18n.changeLanguage(saved.language)
    },
  })

  const handleThemeChange = (v: Theme) => {
    setTheme(v)
    mutation.mutate({ theme: v, language })
  }
  const handleLangChange = (v: Lang) => {
    setLanguage(v)
    mutation.mutate({ theme, language: v })
  }

  return (
    <section className="profile-section">
      <h3>{t('profile.preferences.title')}</h3>

      <div className="field">
        <label className="field-label">{t('profile.preferences.theme')}</label>
        <div className="radio-row">
          {(['light', 'dark', 'system'] as Theme[]).map((v) => (
            <label key={v} className="radio-pill">
              <input type="radio" name="theme" value={v} checked={theme === v}
                     onChange={() => handleThemeChange(v)} />
              <span>{t(`profile.preferences.theme_${v}`)}</span>
            </label>
          ))}
        </div>
      </div>

      <div className="field">
        <label className="field-label" htmlFor="lang-select">{t('profile.preferences.language')}</label>
        <select id="lang-select" className="field-input" value={language}
                onChange={(e) => handleLangChange(e.target.value as Lang)}>
          <option value="en">English</option>
          <option value="fr">Français</option>
        </select>
      </div>
    </section>
  )
}

export default function Profile() {
  const { t } = useTranslation()
  const me = useStore((s) => s.me)

  if (!me) return <div className="page-loading">{t('common.loading')}</div>

  return (
    <div className="page-profile">
      <h2>{t('profile.title')}</h2>

      <section className="profile-section">
        <h3>{t('profile.identity.title')}</h3>
        <div className="field">
          <label className="field-label">{t('profile.identity.email')}</label>
          <div className="field-readonly">{me.email}</div>
        </div>
        <div className="field">
          <label className="field-label">{t('profile.identity.groups')}</label>
          <div className="chip-row">
            {me.groups.length === 0 ? (
              <span className="chip chip-muted">{t('account.no_group')}</span>
            ) : (
              me.groups.map((g) => (
                <span key={g.id} className="chip">{g.name}</span>
              ))
            )}
          </div>
        </div>
      </section>

      <PasswordSection />
      <PreferencesSection />
    </div>
  )
}
