import type { RemoteConfigStatus, AutoRollbackEvent, PushStatus } from '../../types'

interface Props {
  status?: RemoteConfigStatus
  rollback?: AutoRollbackEvent
  onDismissRollback?: () => void
}

export default function PushStatusBanner({ status, rollback, onDismissRollback }: Props) {
  if (!status && !rollback) return null
  return (
    <div className="push-status-stack">
      {status && (
        <div className={`push-banner push-banner-${status.status}`}>
          <div className="push-banner-row">
            <span className="push-banner-label">{label(status.status)}</span>
            <code className="push-banner-hash">{status.config_hash.substring(0, 8)}</code>
          </div>
          {status.error_message && (
            <pre className="push-banner-error">{status.error_message}</pre>
          )}
        </div>
      )}
      {rollback && (
        <div className="push-banner push-banner-rollback">
          <div className="push-banner-row">
            <span className="push-banner-label">Auto-rolled back</span>
            <code className="push-banner-hash">
              {rollback.from_hash.substring(0, 8)} → {rollback.to_hash.substring(0, 8)}
            </code>
            {onDismissRollback && (
              <button className="push-banner-dismiss" onClick={onDismissRollback} aria-label="Dismiss">
                ×
              </button>
            )}
          </div>
          {rollback.reason && <pre className="push-banner-error">{rollback.reason}</pre>}
        </div>
      )}
    </div>
  )
}

function label(s: PushStatus): string {
  switch (s) {
    case 'applying': return 'Applying config...'
    case 'applied':  return '✓ Applied'
    case 'failed':   return '✗ Failed'
    case 'pending':  return 'Pending...'
  }
}
