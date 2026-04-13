import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { configsAPI } from '../api/client'
import YamlEditor from '../components/config/YamlEditor'

export default function Configs() {
  const queryClient = useQueryClient()
  const { data: configs, isLoading } = useQuery({ queryKey: ['configs'], queryFn: configsAPI.list })

  const [name,     setName]     = useState('')
  const [content,  setContent]  = useState('')
  const [showForm, setShowForm] = useState(false)

  const createMutation = useMutation({
    mutationFn: () => configsAPI.create(name, content),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['configs'] })
      setName('')
      setContent('')
      setShowForm(false)
    },
  })

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">Configs</h1>
        <button className={`btn ${showForm ? '' : 'btn-primary'}`} onClick={() => setShowForm(!showForm)}>
          {showForm ? 'Cancel' : '+ New Config'}
        </button>
      </div>

      {showForm && (
        <div className="configs-form">
          <div className="configs-form-header">New configuration</div>
          <div className="configs-form-body">
            <div className="field">
              <label className="field-label">Name</label>
              <input
                className="field-input"
                placeholder="e.g. collector-prod-v2"
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <div className="field" style={{ marginBottom: 0 }}>
              <label className="field-label">Content (YAML)</label>
              <YamlEditor value={content} onChange={setContent} />
            </div>
          </div>
          <div className="configs-form-footer">
            <button
              className="btn btn-primary"
              onClick={() => createMutation.mutate()}
              disabled={!name || !content || createMutation.isPending}
            >
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </button>
          </div>
        </div>
      )}

      {isLoading ? (
        <div className="loading">Loading configs...</div>
      ) : (configs ?? []).length === 0 ? (
        <div className="empty-state">No configurations yet</div>
      ) : (
        <table className="data-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Created by</th>
              <th>Created at</th>
              <th>ID</th>
            </tr>
          </thead>
          <tbody>
            {(configs ?? []).map((c) => (
              <tr key={c.id}>
                <td style={{ fontFamily: 'var(--mono)', color: 'var(--text-hi)' }}>{c.name}</td>
                <td style={{ fontFamily: 'var(--mono)', fontSize: '0.8rem' }}>{c.created_by}</td>
                <td style={{ fontFamily: 'var(--mono)', fontSize: '0.75rem', color: 'var(--muted)', whiteSpace: 'nowrap' }}>
                  {new Date(c.created_at).toLocaleString()}
                </td>
                <td><code>{c.id.substring(0, 12)}...</code></td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
