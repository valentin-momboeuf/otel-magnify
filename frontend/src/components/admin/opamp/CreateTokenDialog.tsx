import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'

import type { CreateOpAMPTokenRequest } from '../../../api/opampTokens'

type CreateTokenDialogProps = {
  errorKind: 'validation' | 'too-large' | 'ambiguous' | null
  isPending: boolean
  onCancel: () => void
  onSubmit: (request: CreateOpAMPTokenRequest) => void
}

export default function CreateTokenDialog({
  errorKind,
  isPending,
  onCancel,
  onSubmit,
}: CreateTokenDialogProps) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [team, setTeam] = useState('')
  const [environment, setEnvironment] = useState('')
  const [expiresAt, setExpiresAt] = useState('')

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    onSubmit({
      name,
      description,
      team,
      environment,
      expires_at: expiresAt ? new Date(expiresAt).toISOString() : '',
    })
  }

  return (
    <div className="opamp-token-dialog-backdrop" role="presentation">
      <section
        className="opamp-token-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="create-opamp-token-title"
      >
        <header>
          <p className="opamp-token-eyebrow">{t('admin.opamp.create.eyebrow')}</p>
          <h2 id="create-opamp-token-title">{t('admin.opamp.create.title')}</h2>
          <p>{t('admin.opamp.create.description')}</p>
        </header>

        {errorKind && (
          <div className={`opamp-token-banner opamp-token-banner-${errorKind}`} role="alert">
            {t(`admin.opamp.create.error.${errorKind}`)}
          </div>
        )}

        <form onSubmit={handleSubmit}>
          <label className="opamp-token-field">
            <span>{t('admin.opamp.field.name')}</span>
            <input value={name} onChange={(event) => setName(event.target.value)} required />
          </label>
          <label className="opamp-token-field">
            <span>{t('admin.opamp.field.description')}</span>
            <textarea
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              rows={3}
            />
          </label>
          <div className="opamp-token-form-grid">
            <label className="opamp-token-field">
              <span>{t('admin.opamp.field.team')}</span>
              <input value={team} onChange={(event) => setTeam(event.target.value)} />
            </label>
            <label className="opamp-token-field">
              <span>{t('admin.opamp.field.environment')}</span>
              <input value={environment} onChange={(event) => setEnvironment(event.target.value)} />
            </label>
          </div>
          <label className="opamp-token-field">
            <span>{t('admin.opamp.field.expires_at')}</span>
            <input
              type="datetime-local"
              value={expiresAt}
              onChange={(event) => setExpiresAt(event.target.value)}
            />
          </label>
          <div className="opamp-token-dialog-actions">
            <button type="button" className="btn" onClick={onCancel} disabled={isPending}>
              {t('common.cancel')}
            </button>
            <button type="submit" className="btn btn-primary" disabled={isPending}>
              {isPending ? t('admin.opamp.create.creating') : t('admin.opamp.create.submit')}
            </button>
          </div>
        </form>
      </section>
    </div>
  )
}
