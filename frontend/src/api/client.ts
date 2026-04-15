import axios from 'axios'
import type { Agent, Config, Alert, AgentConfig } from '../types'

const api = axios.create({ baseURL: '/api' })

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem('token')
      window.location.href = '/login'
    }
    return Promise.reject(err)
  }
)

export const agentsAPI = {
  list: () => api.get<Agent[]>('/agents').then((r) => r.data ?? []),
  get: (id: string) => api.get<Agent>(`/agents/${id}`).then((r) => r.data),
  pushConfig: (id: string, yaml: string) =>
    api
      .post<{ status: string; config_hash: string }>(`/agents/${id}/config`, yaml, {
        headers: { 'Content-Type': 'text/yaml' },
      })
      .then((r) => r.data),
  getConfigHistory: (id: string) =>
    api.get<AgentConfig[]>(`/agents/${id}/configs`).then((r) => r.data ?? []),
}

export const configsAPI = {
  list: () => api.get<Config[]>('/configs').then((r) => r.data ?? []),
  get: (id: string) => api.get<Config>(`/configs/${id}`).then((r) => r.data),
  create: (name: string, content: string) =>
    api.post<Config>('/configs', { name, content }).then((r) => r.data),
}

export const alertsAPI = {
  list: (includeResolved = false) =>
    api.get<Alert[]>('/alerts', { params: { include_resolved: includeResolved } }).then((r) => r.data ?? []),
  resolve: (id: string) => api.post(`/alerts/${id}/resolve`),
}

export const authAPI = {
  login: (email: string, password: string) =>
    api.post<{ token: string }>('/auth/login', { email, password }).then((r) => r.data),
}

export default api
