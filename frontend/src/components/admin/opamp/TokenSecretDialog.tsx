import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

type TokenSecretDialogProps = {
  value: string
  onAcknowledge: () => void
}

export default function TokenSecretDialog({ value, onAcknowledge }: TokenSecretDialogProps) {
  const { t } = useTranslation()
  const valueRef = useRef<HTMLInputElement>(null)
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'failed'>('idle')

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopyState('copied')
    } catch {
      const input = valueRef.current
      input?.focus()
      input?.select()
      setCopyState('failed')
    }
  }

  return (
    <div
      className="opamp-token-dialog-backdrop"
      role="presentation"
      onMouseDown={(event) => event.preventDefault()}
    >
      <section
        className="opamp-token-dialog opamp-token-secret-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="opamp-token-secret-title"
        onKeyDown={(event) => {
          if (event.key === 'Escape') event.preventDefault()
        }}
      >
        <div className="opamp-token-handoff-rail" aria-label={t('admin.opamp.secret.lifecycle')}>
          <strong>{t('admin.opamp.secret.created')}</strong>
          <span aria-hidden />
          <strong>{t('admin.opamp.secret.visible_once')}</strong>
          <span aria-hidden />
          <strong>{t('admin.opamp.secret.cleared')}</strong>
        </div>
        <header>
          <h2 id="opamp-token-secret-title">{t('admin.opamp.secret.title')}</h2>
          <p>{t('admin.opamp.secret.description')}</p>
        </header>
        <label className="opamp-token-field">
          <span>{t('admin.opamp.secret.value_label')}</span>
          <input
            ref={valueRef}
            className="opamp-token-secret-value"
            value={value}
            readOnly
            spellCheck={false}
          />
        </label>
        {copyState === 'copied' && (
          <p className="opamp-token-copy-state opamp-token-copy-success" role="status">
            {t('admin.opamp.secret.copied')}
          </p>
        )}
        {copyState === 'failed' && (
          <p className="opamp-token-banner opamp-token-banner-warning" role="alert">
            {t('admin.opamp.secret.copy_failed')}
          </p>
        )}
        <div className="opamp-token-dialog-actions opamp-token-secret-actions">
          <button type="button" className="btn" onClick={handleCopy}>
            {t('admin.opamp.secret.copy')}
          </button>
          <button type="button" className="btn btn-primary" onClick={onAcknowledge}>
            {t('admin.opamp.secret.acknowledge')}
          </button>
        </div>
      </section>
    </div>
  )
}
