import axios from 'axios'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'

import {
  opampTokensAPI,
  type CreateOpAMPTokenRequest,
  type CreateOpAMPTokenResponse,
  type OpAMPTokenMetadata,
  type OpAMPTokenMutationError,
} from '../../../api/opampTokens'
import { opampTokenKeys } from '../../../api/queryKeys'
import CreateTokenDialog from './CreateTokenDialog'
import TokenSecretDialog from './TokenSecretDialog'

type TokenCreationFlowProps = {
  tokens: OpAMPTokenMetadata[]
  onReconcileToken: (token: OpAMPTokenMetadata) => void
}

type SafeCreateFailure = {
  kind: 'validation' | 'too-large' | 'ambiguous'
  tokenId?: string
}

export default function TokenCreationFlow({ tokens, onReconcileToken }: TokenCreationFlowProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [isPending, setIsPending] = useState(false)
  const [createResponse, setCreateResponse] = useState<CreateOpAMPTokenResponse | null>(null)
  const [failure, setFailure] = useState<SafeCreateFailure | null>(null)

  useEffect(
    () => () => {
      setCreateResponse(null)
    },
    [],
  )

  const refreshMetadata = () =>
    queryClient.invalidateQueries({ queryKey: opampTokenKeys.list(), exact: true })

  const handleSubmit = async (request: CreateOpAMPTokenRequest) => {
    setIsPending(true)
    setFailure(null)
    try {
      const response = await opampTokensAPI.create(request)
      setCreateResponse(response)
      setFormOpen(false)
      void refreshMetadata()
    } catch (error) {
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      const data = axios.isAxiosError<OpAMPTokenMutationError>(error)
        ? error.response?.data
        : undefined
      if (status === 400) {
        setFailure({ kind: 'validation' })
      } else if (status === 413) {
        setFailure({ kind: 'too-large' })
      } else if (
        status === 503 &&
        data?.side_effect_status === 'unknown' &&
        typeof data.token_id === 'string'
      ) {
        setFailure({ kind: 'ambiguous', tokenId: data.token_id })
        setFormOpen(false)
        await refreshMetadata()
      } else {
        setFailure({ kind: 'ambiguous' })
        await refreshMetadata()
      }
    } finally {
      setIsPending(false)
    }
  }

  const affectedToken = failure?.tokenId
    ? tokens.find((token) => token.id === failure.tokenId)
    : undefined
  const affectedTokenIsRevocable =
    affectedToken?.status === 'active' || affectedToken?.status === 'expired'

  return (
    <>
      <button className="btn btn-primary" type="button" onClick={() => setFormOpen(true)}>
        {t('admin.opamp.create.open')}
      </button>

      {formOpen && !createResponse && (
        <CreateTokenDialog
          errorKind={failure?.kind ?? null}
          isPending={isPending}
          onCancel={() => {
            setFailure(null)
            setFormOpen(false)
          }}
          onSubmit={handleSubmit}
        />
      )}

      {failure?.tokenId && affectedToken && !formOpen && (
        <div className="opamp-token-banner opamp-token-banner-warning" role="alert">
          {affectedToken.status === 'revoked'
            ? t('admin.opamp.create.reconciliation.revoked')
            : t('admin.opamp.create.reconciliation.found', { id: affectedToken.id })}
          {affectedTokenIsRevocable && (
            <button
              className="btn btn-danger"
              type="button"
              onClick={() => onReconcileToken(affectedToken)}
            >
              {t('admin.opamp.create.reconciliation.revoke')}
            </button>
          )}
        </div>
      )}

      {failure?.tokenId && !affectedToken && !formOpen && (
        <div className="opamp-token-banner opamp-token-banner-warning" role="alert">
          {t('admin.opamp.create.reconciliation.absent', { id: failure.tokenId })}
          <button
            className="btn"
            type="button"
            onClick={() => {
              setFailure(null)
              setFormOpen(true)
            }}
          >
            {t('admin.opamp.create.reconciliation.retry')}
          </button>
        </div>
      )}

      {createResponse && (
        <TokenSecretDialog
          value={createResponse.value}
          onAcknowledge={() => {
            setCreateResponse(null)
            setFailure(null)
          }}
        />
      )}
    </>
  )
}
