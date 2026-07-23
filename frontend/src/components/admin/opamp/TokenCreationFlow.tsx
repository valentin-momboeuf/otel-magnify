import axios from 'axios'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useBlocker, useBeforeUnload } from 'react-router-dom'
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

type SafeCreateFailure = 'validation' | 'too-large' | 'ambiguous'
type RefreshState = 'refreshing' | 'refresh-failed' | 'succeeded'
type ExactOutcome = 'found-active-or-expired' | 'revoked' | 'absent'

type Reconciliation =
  | {
      kind: 'generic'
      refreshState: RefreshState
    }
  | {
      kind: 'exact'
      refreshState: Exclude<RefreshState, 'succeeded'>
      tokenId: string
    }
  | {
      kind: 'exact'
      refreshState: 'succeeded'
      outcome: ExactOutcome
      token?: OpAMPTokenMetadata
      tokenId: string
    }

function classifyExactOutcome(
  tokens: OpAMPTokenMetadata[],
  tokenId: string,
): Pick<
  Extract<Reconciliation, { kind: 'exact'; refreshState: 'succeeded' }>,
  'outcome' | 'token'
> {
  const token = tokens.find((candidate) => candidate.id === tokenId)
  if (!token) return { outcome: 'absent' }
  if (token.status === 'revoked') return { outcome: 'revoked', token }
  return { outcome: 'found-active-or-expired', token }
}

export default function TokenCreationFlow({ tokens, onReconcileToken }: TokenCreationFlowProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [formOpen, setFormOpen] = useState(false)
  const [isPending, setIsPending] = useState(false)
  const [createResponse, setCreateResponse] = useState<CreateOpAMPTokenResponse | null>(null)
  const [failure, setFailure] = useState<SafeCreateFailure | null>(null)
  const [reconciliation, setReconciliation] = useState<Reconciliation | null>(null)

  const navigationUnsafe = isPending || Boolean(createResponse)
  const blocker = useBlocker(navigationUnsafe)
  useBeforeUnload(
    useCallback(
      (event) => {
        if (!navigationUnsafe) return
        event.preventDefault()
        event.returnValue = ''
      },
      [navigationUnsafe],
    ),
  )
  useEffect(() => {
    if (blocker.state === 'blocked' && !navigationUnsafe) blocker.reset()
  }, [blocker, navigationUnsafe])

  const refreshMetadata = useCallback(async () => {
    const refreshedTokens = await opampTokensAPI.list()
    queryClient.setQueryData(opampTokenKeys.list(), refreshedTokens)
    return refreshedTokens
  }, [queryClient])

  const refreshExact = useCallback(
    async (tokenId: string) => {
      setReconciliation({ kind: 'exact', refreshState: 'refreshing', tokenId })
      try {
        const refreshedTokens = await refreshMetadata()
        setReconciliation({
          kind: 'exact',
          refreshState: 'succeeded',
          tokenId,
          ...classifyExactOutcome(refreshedTokens, tokenId),
        })
      } catch {
        setReconciliation({ kind: 'exact', refreshState: 'refresh-failed', tokenId })
      }
    },
    [refreshMetadata],
  )

  const refreshGeneric = useCallback(async () => {
    setReconciliation({ kind: 'generic', refreshState: 'refreshing' })
    try {
      await refreshMetadata()
      setReconciliation({ kind: 'generic', refreshState: 'succeeded' })
    } catch {
      setReconciliation({ kind: 'generic', refreshState: 'refresh-failed' })
    }
  }, [refreshMetadata])

  const handleSubmit = async (request: CreateOpAMPTokenRequest) => {
    setIsPending(true)
    setFailure(null)
    setReconciliation(null)
    try {
      const response = await opampTokensAPI.create(request)
      setCreateResponse(response)
      setFormOpen(false)
      void refreshMetadata().catch(() => undefined)
    } catch (error) {
      const status = axios.isAxiosError(error) ? error.response?.status : undefined
      const data = axios.isAxiosError<OpAMPTokenMutationError>(error)
        ? error.response?.data
        : undefined
      if (status === 400) {
        setFailure('validation')
      } else if (status === 413) {
        setFailure('too-large')
      } else if (
        status === 503 &&
        data?.side_effect_status === 'unknown' &&
        typeof data.token_id === 'string'
      ) {
        setFailure('ambiguous')
        setFormOpen(false)
        setIsPending(false)
        await refreshExact(data.token_id)
      } else {
        setFailure('ambiguous')
        setIsPending(false)
        await refreshGeneric()
      }
    } finally {
      setIsPending(false)
    }
  }

  const currentExactToken =
    reconciliation?.kind === 'exact' && reconciliation.refreshState === 'succeeded'
      ? (tokens.find((token) => token.id === reconciliation.tokenId) ?? reconciliation.token)
      : undefined
  const currentExactOutcome =
    currentExactToken?.status === 'revoked'
      ? 'revoked'
      : reconciliation?.kind === 'exact' && reconciliation.refreshState === 'succeeded'
        ? reconciliation.outcome
        : undefined
  const exactReconciliationUnsafe =
    reconciliation?.kind === 'exact' &&
    (reconciliation.refreshState !== 'succeeded' ||
      currentExactOutcome === 'found-active-or-expired')
  const genericRefreshState =
    reconciliation?.kind === 'generic' ? reconciliation.refreshState : null
  const genericSubmissionBlocked =
    genericRefreshState === 'refreshing' || genericRefreshState === 'refresh-failed'
  const genericReconciliationUnsafe =
    reconciliation?.kind === 'generic' && reconciliation.refreshState !== 'succeeded'

  return (
    <>
      <button
        className="btn btn-primary"
        type="button"
        disabled={exactReconciliationUnsafe || genericReconciliationUnsafe}
        onClick={() => {
          setFailure(null)
          setReconciliation(null)
          setFormOpen(true)
        }}
      >
        {t('admin.opamp.create.open')}
      </button>

      {formOpen && !createResponse && (
        <CreateTokenDialog
          errorKind={failure}
          isPending={isPending}
          onCancel={() => {
            if (reconciliation?.kind !== 'generic') {
              setFailure(null)
              setReconciliation(null)
            }
            setFormOpen(false)
          }}
          onRetryRefresh={refreshGeneric}
          onSubmit={handleSubmit}
          refreshState={genericRefreshState}
          submissionBlocked={genericSubmissionBlocked}
        />
      )}

      {reconciliation?.kind === 'generic' && !formOpen && (
        <div
          className={`opamp-token-banner ${
            reconciliation.refreshState === 'refresh-failed'
              ? 'opamp-token-banner-error'
              : 'opamp-token-banner-warning'
          }`}
          role="alert"
        >
          <span>
            {reconciliation.refreshState === 'refreshing' &&
              t('admin.opamp.reconciliation.refreshing')}
            {reconciliation.refreshState === 'refresh-failed' &&
              t('admin.opamp.reconciliation.refresh_failed')}
            {reconciliation.refreshState === 'succeeded' && t('admin.opamp.create.error.ambiguous')}
          </span>
          {reconciliation.refreshState === 'refresh-failed' && (
            <button className="btn" type="button" onClick={refreshGeneric}>
              {t('admin.opamp.reconciliation.retry_refresh')}
            </button>
          )}
        </div>
      )}

      {reconciliation?.kind === 'exact' && !formOpen && (
        <div
          className={`opamp-token-banner ${
            reconciliation.refreshState === 'refresh-failed'
              ? 'opamp-token-banner-error'
              : 'opamp-token-banner-warning'
          }`}
          role="alert"
        >
          <span>
            {reconciliation.refreshState === 'refreshing' &&
              t('admin.opamp.reconciliation.refreshing')}
            {reconciliation.refreshState === 'refresh-failed' &&
              t('admin.opamp.reconciliation.refresh_failed')}
            {reconciliation.refreshState === 'succeeded' &&
              currentExactOutcome === 'found-active-or-expired' &&
              t('admin.opamp.create.reconciliation.found', { id: reconciliation.tokenId })}
            {reconciliation.refreshState === 'succeeded' &&
              currentExactOutcome === 'revoked' &&
              t('admin.opamp.create.reconciliation.revoked')}
            {reconciliation.refreshState === 'succeeded' &&
              currentExactOutcome === 'absent' &&
              t('admin.opamp.create.reconciliation.absent', { id: reconciliation.tokenId })}
          </span>
          {reconciliation.refreshState === 'refresh-failed' && (
            <button
              className="btn"
              type="button"
              onClick={() => refreshExact(reconciliation.tokenId)}
            >
              {t('admin.opamp.reconciliation.retry_refresh')}
            </button>
          )}
          {reconciliation.refreshState === 'succeeded' &&
            currentExactOutcome === 'found-active-or-expired' &&
            currentExactToken && (
              <button
                className="btn btn-danger"
                type="button"
                onClick={() => onReconcileToken(currentExactToken)}
              >
                {t('admin.opamp.create.reconciliation.revoke')}
              </button>
            )}
        </div>
      )}

      {createResponse && (
        <TokenSecretDialog
          navigationBlocked={blocker.state === 'blocked'}
          value={createResponse.value}
          onAcknowledge={() => {
            if (blocker.state === 'blocked') blocker.reset()
            setCreateResponse(null)
            setFailure(null)
          }}
        />
      )}
    </>
  )
}
