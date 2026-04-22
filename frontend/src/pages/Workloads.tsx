import { useState, useEffect, useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { workloadsAPI } from '../api/client'
import WorkloadCard from '../components/workloads/WorkloadCard'
import { isSupervised, isReadOnlyCollector } from '../lib/workloadCapabilities'
import '../styles/inventory.css'

type ControlFilter = '' | 'supervised' | 'readonly'

export default function Inventory() {
  const { t } = useTranslation()
  const { data: workloads, isLoading } = useQuery({
    queryKey: ['workloads'],
    queryFn: () => workloadsAPI.list(),
  })
  const [searchParams] = useSearchParams()

  const [search,        setSearch]        = useState('')
  const [filterType,    setFilterType]    = useState<string>(searchParams.get('type') ?? '')
  const [filterStatus,  setFilterStatus]  = useState<string>('')
  const [filterControl, setFilterControl] = useState<ControlFilter>(
    (searchParams.get('control') as ControlFilter) ?? ''
  )

  useEffect(() => {
    const type = searchParams.get('type')
    if (type) setFilterType(type)
    const control = searchParams.get('control') as ControlFilter | null
    if (control) setFilterControl(control)
  }, [searchParams])

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase()
    return (workloads ?? []).filter((w) => {
      if (filterType   && w.type   !== filterType)   return false
      if (filterStatus && w.status !== filterStatus) return false
      if (filterControl) {
        if (w.type !== 'collector')                                    return false
        if (filterControl === 'supervised' && !isSupervised(w))        return false
        if (filterControl === 'readonly'   && !isReadOnlyCollector(w)) return false
      }
      if (needle) {
        const haystack = `${w.display_name ?? ''} ${w.id}`.toLowerCase()
        if (!haystack.includes(needle)) return false
      }
      return true
    })
  }, [workloads, search, filterType, filterStatus, filterControl])

  return (
    <div>
      <header className="page-header">
        <h1 className="page-title">{t('inventory.title')}</h1>
        <span className="page-header-count">
          {filtered.length} / {workloads?.length ?? 0}
        </span>
      </header>

      <div className="filter-bar">
        <input
          className="search-input"
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t('inventory.filter.search_placeholder')}
        />
        <select className="filter-select" value={filterType} onChange={(e) => setFilterType(e.target.value)}>
          <option value="">{t('inventory.filter.type.all')}</option>
          <option value="collector">{t('inventory.filter.type.collector')}</option>
          <option value="sdk">{t('inventory.filter.type.sdk')}</option>
        </select>
        <select className="filter-select" value={filterStatus} onChange={(e) => setFilterStatus(e.target.value)}>
          <option value="">{t('inventory.filter.status.all')}</option>
          <option value="connected">{t('inventory.filter.status.connected')}</option>
          <option value="disconnected">{t('inventory.filter.status.disconnected')}</option>
          <option value="degraded">{t('inventory.filter.status.degraded')}</option>
        </select>
        <select
          className="filter-select"
          value={filterControl}
          onChange={(e) => setFilterControl(e.target.value as ControlFilter)}
        >
          <option value="">{t('inventory.filter.control.all')}</option>
          <option value="supervised">{t('inventory.filter.control.supervised')}</option>
          <option value="readonly">{t('inventory.filter.control.readonly')}</option>
        </select>
      </div>

      {isLoading ? (
        <div className="loading">{t('common.loading')}</div>
      ) : filtered.length === 0 ? (
        <div className="empty-state">{t('inventory.empty')}</div>
      ) : (
        filtered.map((w) => <WorkloadCard key={w.id} workload={w} />)
      )}
    </div>
  )
}
