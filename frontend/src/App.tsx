import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'
import { useEffect } from 'react'
import Layout from './components/layout/Layout'
import ProtectedRoute from './components/ProtectedRoute'
import Dashboard from './pages/Dashboard'
import Workloads from './pages/Workloads'
import WorkloadDetail from './pages/WorkloadDetail'
import Configs from './pages/Configs'
import Alerts from './pages/Alerts'
import Profile from './pages/Profile'
import Admin from './pages/Admin'
import Login from './pages/Login'
import { connectWS, disconnectWS } from './api/websocket'
import { queryClient } from './api/queryClient'
import { meAPI } from './api/client'
import { useStore } from './store'
import { useTheme } from './hooks/useTheme'

function AppShell() {
  useTheme()
  const setMe = useStore((s) => s.setMe)

  useEffect(() => {
    connectWS()
    meAPI.get().then(setMe).catch(() => {
      // 401 → already handled by the axios interceptor (redirect /login).
    })
    return () => disconnectWS()
  }, [setMe])

  return (
    <Routes>
      <Route element={<ProtectedRoute><Layout /></ProtectedRoute>}>
        <Route path="/" element={<Dashboard />} />
        <Route path="/inventory" element={<Workloads />} />
        <Route path="/workloads/:id" element={<WorkloadDetail />} />
        <Route path="/configs" element={<Configs />} />
        <Route path="/alerts" element={<Alerts />} />
        <Route path="/profile" element={<Profile />} />
        <Route path="/admin" element={<Admin />} />
      </Route>
      <Route path="/login" element={<Login />} />
      <Route path="*" element={<Navigate to="/" />} />
    </Routes>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AppShell />
      </BrowserRouter>
    </QueryClientProvider>
  )
}
