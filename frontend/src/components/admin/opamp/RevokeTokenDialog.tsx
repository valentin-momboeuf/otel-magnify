import axios from 'axios'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQueryClient } from '@tanstack/react-query'

import {
  opampTokensAPI,
  type OpAMPTokenMetadata,
  type OpAMPTokenMutationError,
} from '../../../api/opampTokens'
import { opampTokenKeys } from '../../../api/queryKeys'
import AdminDialog from './AdminDialog'

type RevokeTokenDialogProps = {
  token: OpAMPTokenMetadata
  onClose: () => void
}

type RevokeState = 'confirm' | 'generic-error' | 'success'
type RevokeReconciliation =
  | { state: 'refreshing' | 'refresh-failed' }
  | { state: 'found-active-or-expired' | 'revoked' | 'absent' }

function classifyRevokeReconciliation(
  tokens: OpAMPTokenMetadata[],
  tokenId: string,
): RevokeReconciliation {
  const refreshedToken = tokens.find((candidate) => candidate.id === tokenId)
  if (!refreshedToken) return { state: 'absent' }
  if (refreshedToken.status === 'revoked') return { state: 'revoked' }
  return { state: 'found-active-or-expired' }
}

export default function RevokeTokenDialog({ token, onClose }: RevokeTokenDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const cancelRef = useRef<HTMLButtonElement>(null)
  const [state, setState] = useState<RevokeState>('confirm')
  const [reconciliation, setReconciliation] = useState<RevokeReconciliation | null>(null)

  const refreshMetadata = async () => {
    const refreshedTokens = await opampTokensAPI.list()
    queryClient.setQueryData(opampTokenKeys.list(), refreshedTokens)
    return refreshedTokens
  }

  const refreshUnknown = async () => {
    setReconciliation({ state: 'refreshing' })
    try {
      const refreshedTokens = await refreshMetadata()
      setReconciliation(classifyRevokeReconciliation(refreshedTokens, token.id))
    } catch {
      setReconciliation({ state: 'refresh-failed' })
    }
  }

  const revoke = useMutation({
    mutationFn: () => opampTokensAPI.revoke(token.id),
    onSuccess: async () => {
      setReconciliation(null)
      setState('success')
      await refreshMetadata().catch(() => undefined)
    },
    onError: async (error) => {
      const data = axios.isAxiosError<OpAMPTokenMutationError>(error)
        ? error.response?.data
        : undefined
      if (data?.side_effect_status === 'unknown') {
        await refreshUnknown()
        return
      }
      setReconciliation(null)
      setState('generic-error')
    },
  })

  const retryRevoke = () => revoke.mutate()
  const requestClose = () => {
    if (!revoke.isPending) onClose()
  }

  return (
    <AdminDialog
      ariaLabelledby="revoke-opamp-token-title"
      className="opamp-token-revoke-dialog"
      initialFocusRef={cancelRef}
      onRequestClose={requestClose}
      preventDismiss={revoke.isPending}
    >
      <p className="opamp-token-eyebrow">{t('admin.opamp.revoke.eyebrow')}</p>
      <h2 id="revoke-opamp-token-title">{t('admin.opamp.revoke.title')}</h2>
      <p>{t('admin.opamp.revoke.description', { name: token.name })}</p>
      <code className="opamp-token-public-id">{token.id}</code>

      {state === 'generic-error' && (
        <div className="opamp-token-banner opamp-token-banner-error" role="alert">
          <span>{t('admin.opamp.revoke.error.generic')}</span>
          <div className="opamp-token-inline-actions">
            <button className="btn" type="button" onClick={retryRevoke} disabled={revoke.isPending}>
              {t('common.retry')}
            </button>
            <button
              className="btn"
              type="button"
              onClick={requestClose}
              disabled={revoke.isPending}
            >
              {t('common.cancel')}
            </button>
          </div>
        </div>
      )}

      {reconciliation && (
        <div
          className={`opamp-token-banner ${
            reconciliation.state === 'refresh-failed'
              ? 'opamp-token-banner-error'
              : 'opamp-token-banner-warning'
          }`}
          role="alert"
        >
          <span>
            {reconciliation.state === 'refreshing' && t('admin.opamp.reconciliation.refreshing')}
            {reconciliation.state === 'refresh-failed' &&
              t('admin.opamp.reconciliation.refresh_failed')}
            {reconciliation.state === 'found-active-or-expired' &&
              t('admin.opamp.revoke.unknown.active')}
            {reconciliation.state === 'revoked' && t('admin.opamp.revoke.unknown.reconciled')}
            {reconciliation.state === 'absent' && t('admin.opamp.revoke.unknown.absent')}
          </span>
          <div className="opamp-token-inline-actions">
            {reconciliation.state === 'refresh-failed' && (
              <button className="btn" type="button" onClick={refreshUnknown}>
                {t('admin.opamp.reconciliation.retry_refresh')}
              </button>
            )}
            {reconciliation.state === 'found-active-or-expired' && (
              <button
                className="btn btn-danger"
                type="button"
                onClick={retryRevoke}
                disabled={revoke.isPending}
              >
                {t('admin.opamp.revoke.unknown.retry')}
              </button>
            )}
            {(reconciliation.state === 'refresh-failed' ||
              reconciliation.state === 'found-active-or-expired') && (
              <button
                className="btn"
                type="button"
                onClick={requestClose}
                disabled={revoke.isPending}
              >
                {t('common.cancel')}
              </button>
            )}
          </div>
        </div>
      )}

      {state === 'success' && (
        <p className="opamp-token-banner opamp-token-banner-success" role="status">
          {t('admin.opamp.revoke.success')}
        </p>
      )}

      <div className="opamp-token-dialog-actions">
        {state === 'confirm' && !reconciliation && (
          <>
            <button
              ref={cancelRef}
              className="btn"
              type="button"
              onClick={requestClose}
              disabled={revoke.isPending}
            >
              {t('common.cancel')}
            </button>
            <button
              className="btn btn-danger"
              type="button"
              onClick={retryRevoke}
              disabled={revoke.isPending}
            >
              {t('admin.opamp.revoke.confirm')}
            </button>
          </>
        )}
        {(state === 'success' ||
          reconciliation?.state === 'revoked' ||
          reconciliation?.state === 'absent') && (
          <button
            className="btn btn-primary"
            type="button"
            onClick={requestClose}
            disabled={revoke.isPending}
          >
            {t('admin.opamp.revoke.done')}
          </button>
        )}
      </div>
    </AdminDialog>
  )
}
