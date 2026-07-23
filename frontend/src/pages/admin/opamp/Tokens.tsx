import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Navigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'

import { opampTokensAPI, type OpAMPTokenMetadata } from '../../../api/opampTokens'
import { opampTokenKeys } from '../../../api/queryKeys'
import TokenCreationFlow from '../../../components/admin/opamp/TokenCreationFlow'
import RevokeTokenDialog from '../../../components/admin/opamp/RevokeTokenDialog'
import { hasPerm } from '../../../lib/perm'
import { useStore } from '../../../store'
import '../../../styles/admin-opamp-tokens.css'

function formatDate(value: string | undefined, language: string): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat(language, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

export default function Tokens() {
  const { t, i18n } = useTranslation()
  const me = useStore((state) => state.me)
  const [revokeTarget, setRevokeTarget] = useState<OpAMPTokenMetadata | null>(null)

  const list = useQuery({
    queryKey: opampTokenKeys.list(),
    queryFn: opampTokensAPI.list,
    enabled: Boolean(me) && hasPerm(me?.groups, 'settings:manage'),
  })

  if (!me) return null
  if (!hasPerm(me.groups, 'settings:manage')) return <Navigate to="/" replace />

  const tokens = list.data ?? []
  const revokeToken = revokeTarget
    ? (tokens.find((token) => token.id === revokeTarget.id) ?? revokeTarget)
    : null

  return (
    <div className="page-admin-opamp-tokens">
      <header className="opamp-token-page-header">
        <div>
          <p className="opamp-token-breadcrumb">
            {t('admin.title')} / {t('admin.opamp.title')}
          </p>
          <h1>{t('admin.opamp.title')}</h1>
          <p>{t('admin.opamp.subtitle')}</p>
        </div>
        <div className="opamp-token-page-actions">
          <button
            className="btn"
            type="button"
            onClick={() => list.refetch()}
            disabled={list.isFetching}
          >
            {t('admin.opamp.refresh')}
          </button>
          <TokenCreationFlow tokens={tokens} onReconcileToken={setRevokeTarget} />
        </div>
      </header>

      <aside className="opamp-token-rotation-note">
        <strong>{t('admin.opamp.rotation.title')}</strong>
        <span>{t('admin.opamp.rotation.description')}</span>
      </aside>

      {list.isLoading && <p>{t('common.loading')}</p>}
      {list.isError && (
        <div className="opamp-token-banner opamp-token-banner-error" role="alert">
          <span>{t('admin.opamp.error.list')}</span>
          <button className="btn" type="button" onClick={() => list.refetch()}>
            {t('common.retry')}
          </button>
        </div>
      )}
      {list.isSuccess && tokens.length === 0 && (
        <div className="opamp-token-empty-state">
          <strong>{t('admin.opamp.empty.title')}</strong>
          <p>{t('admin.opamp.empty.description')}</p>
        </div>
      )}
      {list.isSuccess && tokens.length > 0 && (
        <div className="opamp-token-table-scroll">
          <table className="opamp-token-table" data-testid="opamp-token-table">
            <thead>
              <tr>
                <th>{t('admin.opamp.column.name')}</th>
                <th>{t('admin.opamp.column.team')}</th>
                <th>{t('admin.opamp.column.environment')}</th>
                <th>{t('admin.opamp.column.created')}</th>
                <th>{t('admin.opamp.column.expires')}</th>
                <th>{t('admin.opamp.column.last_used')}</th>
                <th>{t('admin.opamp.column.status')}</th>
                <th>{t('admin.opamp.column.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {tokens.map((token) => (
                <TokenRow
                  key={token.id}
                  token={token}
                  language={i18n.resolvedLanguage ?? i18n.language}
                  onRevoke={() => setRevokeTarget(token)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {revokeToken && (
        <RevokeTokenDialog token={revokeToken} onClose={() => setRevokeTarget(null)} />
      )}
    </div>
  )
}

function TokenRow({
  token,
  language,
  onRevoke,
}: {
  token: OpAMPTokenMetadata
  language: string
  onRevoke: () => void
}) {
  const { t } = useTranslation()
  const revocable = token.status === 'active' || token.status === 'expired'

  return (
    <tr data-testid={`opamp-token-row-${token.id}`}>
      <td className="opamp-token-name-cell">
        <strong>{token.name}</strong>
        {token.description && <span>{token.description}</span>}
        <code>{token.id}</code>
      </td>
      <td>{token.team || '—'}</td>
      <td>{token.environment || '—'}</td>
      <td>
        <time dateTime={token.created_at}>{formatDate(token.created_at, language)}</time>
        <span className="opamp-token-created-by">{token.created_by}</span>
      </td>
      <td>
        {token.expires_at ? (
          <time dateTime={token.expires_at}>{formatDate(token.expires_at, language)}</time>
        ) : (
          '—'
        )}
      </td>
      <td>
        {token.last_used_at ? (
          <time dateTime={token.last_used_at}>{formatDate(token.last_used_at, language)}</time>
        ) : (
          '—'
        )}
      </td>
      <td>
        <span className={`opamp-token-status opamp-token-status-${token.status}`}>
          {t(`admin.opamp.status.${token.status}`)}
        </span>
      </td>
      <td>
        {revocable ? (
          <button className="btn btn-danger" type="button" onClick={onRevoke}>
            {t('admin.opamp.revoke.action')}
          </button>
        ) : (
          '—'
        )}
      </td>
    </tr>
  )
}
