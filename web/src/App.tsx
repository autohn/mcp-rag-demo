import { lazy, Suspense } from 'react'
import { Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Spinner } from '@/components/ui'
import Layout from '@/components/Layout'
import AuthPage from '@/pages/AuthPage'
import ProjectsPage from '@/pages/ProjectsPage'
import ProjectPage from '@/pages/ProjectPage'
import TokensPage from '@/pages/TokensPage'

const DocumentPage = lazy(() => import('@/pages/DocumentPage'))

export function useMe() {
  return useQuery({ queryKey: ['me'], queryFn: api.me, retry: false, staleTime: Infinity })
}

function RequireAuth({ children }: { children: React.ReactNode }) {
  const me = useMe()
  const loc = useLocation()
  if (me.isLoading) {
    return <div className="flex h-full items-center justify-center"><Spinner /></div>
  }
  if (!me.data) return <Navigate to="/login" state={{ from: loc.pathname + loc.search }} replace />
  return <Layout user={me.data}>{children}</Layout>
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<AuthPage mode="login" />} />
      <Route path="/register" element={<AuthPage mode="register" />} />
      <Route path="/" element={<RequireAuth><ProjectsPage /></RequireAuth>} />
      <Route path="/projects/:id/:tab?" element={<RequireAuth><ProjectPage /></RequireAuth>} />
      <Route path="/documents/:id" element={<RequireAuth><Suspense fallback={<Spinner />}><DocumentPage /></Suspense></RequireAuth>} />
      <Route path="/settings/tokens" element={<RequireAuth><TokensPage /></RequireAuth>} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
