import axios from 'axios'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMutation, useQueryClient } from '@tanstack/react-query'

import {
  opampTokensAPI,
  type OpAMPTokenMetadata,
  type OpAMPTokenMutationError,
} from '../../../api/opampTokens'
import { opampTokenKeys } from '../../../api/queryKeys'

type RevokeTokenDialogProps = {
  token: OpAMPTokenMetadata
  onClose: () => void
}

type RevokeState = 'confirm' | 'generic-error' | 'unknown' | 'success'

export default function RevokeTokenDialog({ token, onClose }: RevokeTokenDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [state, setState] = useState<RevokeState>('confirm')

  const refreshMetadata = () =>
    queryClient.invalidateQueries({ queryKey: opampTokenKeys.list(), exact: true })

  const revoke = useMutation({
    mutationFn: () => opampTokensAPI.revoke(token.id),
    onSuccess: async () => {
      setState('success')
      await refreshMetadata()
    },
    onError: async (error) => {
      const data = axios.isAxiosError<OpAMPTokenMutationError>(error)
        ? error.response?.data
        : undefined
      if (data?.side_effect_status === 'unknown') {
        setState('unknown')
        await refreshMetadata()
        return
      }
      setState('generic-error')
    },
  })

  const tokenStillRevocable = token.status === 'active' || token.status === 'expired'

  return (
    <div className="opamp-token-dialog-backdrop" role="presentation">
      <section
        className="opamp-token-dialog opamp-token-revoke-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="revoke-opamp-token-title"
      >
        <p className="opamp-token-eyebrow">{t('admin.opamp.revoke.eyebrow')}</p>
        <h2 id="revoke-opamp-token-title">{t('admin.opamp.revoke.title')}</h2>
        <p>{t('admin.opamp.revoke.description', { name: token.name })}</p>
        <code className="opamp-token-public-id">{token.id}</code>

        {state === 'generic-error' && (
          <div className="opamp-token-banner opamp-token-banner-error" role="alert">
            {t('admin.opamp.revoke.error.generic')}
            <button className="btn" type="button" onClick={() => revoke.mutate()}>
              {t('common.retry')}
            </button>
          </div>
        )}

        {state === 'unknown' && (
          <div className="opamp-token-banner opamp-token-banner-warning" role="alert">
            {token.status === 'revoked'
              ? t('admin.opamp.revoke.unknown.reconciled')
              : t('admin.opamp.revoke.unknown.active')}
            {tokenStillRevocable && (
              <button className="btn btn-danger" type="button" onClick={() => revoke.mutate()}>
                {t('admin.opamp.revoke.unknown.retry')}
              </button>
            )}
          </div>
        )}

        {state === 'success' && (
          <p className="opamp-token-banner opamp-token-banner-success" role="status">
            {t('admin.opamp.revoke.success')}
          </p>
        )}

        <div className="opamp-token-dialog-actions">
          {state === 'confirm' && (
            <>
              <button className="btn" type="button" onClick={onClose}>
                {t('common.cancel')}
              </button>
              <button
                className="btn btn-danger"
                type="button"
                onClick={() => revoke.mutate()}
                disabled={revoke.isPending}
              >
                {t('admin.opamp.revoke.confirm')}
              </button>
            </>
          )}
          {(state === 'success' || (state === 'unknown' && token.status === 'revoked')) && (
            <button className="btn btn-primary" type="button" onClick={onClose}>
              {t('admin.opamp.revoke.done')}
            </button>
          )}
        </div>
      </section>
    </div>
  )
}
