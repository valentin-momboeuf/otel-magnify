import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useEffect } from 'react'
import Layout from './components/layout/Layout'
import ProtectedRoute from './components/ProtectedRoute'
import Dashboard from './pages/Dashboard'
import Inventory from './pages/Agents'
import AgentDetail from './pages/AgentDetail'
import Configs from './pages/Configs'
import Alerts from './pages/Alerts'
import Login from './pages/Login'
import { connectWS, disconnectWS } from './api/websocket'

const queryClient = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: false, retry: 1 } },
})

function AppShell() {
  useEffect(() => {
    connectWS()
    return () => disconnectWS()
  }, [])

  return (
    <Routes>
      <Route element={<ProtectedRoute><Layout /></ProtectedRoute>}>
        <Route path="/" element={<Dashboard />} />
        <Route path="/inventory" element={<Inventory />} />
        <Route path="/inventory/:id" element={<AgentDetail />} />
        <Route path="/configs" element={<Configs />} />
        <Route path="/alerts" element={<Alerts />} />
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
