import type { Workload } from '../types'

/** Collector with AcceptsRemoteConfig capability — eligible for config push. */
export function isSupervised(workload: Workload): boolean {
  return workload.type === 'collector' && workload.accepts_remote_config === true
}

/**
 * Collector without AcceptsRemoteConfig — Edit is hidden, push returns 409.
 * Not defined as !isSupervised: SDK workloads are neither supervised nor read-only collectors.
 * Treats missing accepts_remote_config as read-only (safe default for pre-migration payloads).
 */
export function isReadOnlyCollector(workload: Workload): boolean {
  return workload.type === 'collector' && workload.accepts_remote_config !== true
}
