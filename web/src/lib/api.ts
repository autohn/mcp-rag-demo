export type Role = 'admin' | 'participant'

export interface User { id: number; email: string; name: string }
export interface Project { id: number; name: string; role: Role; created_at: string }
export interface Member { user_id: number; email: string; name: string; role: Role }
export interface Folder { id: number; project_id: number; parent_id: number | null; name: string }
export interface Document {
  id: number
  project_id: number
  folder_id: number | null
  name: string
  kind: 'pdf' | 'docx' | 'txt' | 'md'
  mime: string
  size: number
  status: 'processing' | 'ready' | 'failed'
  error?: string
  page_count: number
  chunk_count: number
  paged: boolean
  no_text: boolean
  uploaded_by: number | null
  uploaded_by_name: string
  created_at: string
}
export interface FolderListing { folder: Folder | null; path: Folder[]; folders: Folder[]; documents: Document[] }
export interface Page { page: number; text: string }
export interface Citation {
  n: number
  document_id: number | null
  document_name: string
  page: number
  paged: boolean
  quote: string
  document_deleted: boolean
}
export interface QuestionRecord {
  id: number
  project_id: number
  folder_id: number | null
  question: string
  answer: string
  found: boolean
  citations: Citation[]
  created_at: string
}
export interface AccessToken { id: number; name: string; created_at: string; last_used_at: string | null; token?: string }
export interface ServerConfig { llm_enabled: boolean; max_upload_mb: number }

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, credentials: 'same-origin', headers: {} }
  if (body instanceof FormData) {
    init.body = body
  } else if (body !== undefined) {
    init.body = JSON.stringify(body)
    ;(init.headers as Record<string, string>)['Content-Type'] = 'application/json'
  }
  const res = await fetch(url, init)
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, data.error ?? `Error ${res.status}`)
  return data as T
}

const get = <T>(url: string) => request<T>('GET', url)
const q = (v: number | null | undefined) => (v == null ? 'root' : String(v))

export const api = {
  config: () => get<ServerConfig>('/api/config'),
  me: () => get<User>('/api/me'),
  login: (email: string, password: string) => request<User>('POST', '/api/auth/login', { email, password }),
  register: (email: string, password: string, name: string) =>
    request<User>('POST', '/api/auth/register', { email, password, name }),
  logout: () => request<void>('POST', '/api/auth/logout'),

  projects: () => get<Project[]>('/api/projects'),
  project: (id: number) => get<Project>(`/api/projects/${id}`),
  createProject: (name: string) => request<Project>('POST', '/api/projects', { name }),
  renameProject: (id: number, name: string) => request<Project>('PATCH', `/api/projects/${id}`, { name }),
  deleteProject: (id: number) => request<void>('DELETE', `/api/projects/${id}`),

  members: (pid: number) => get<Member[]>(`/api/projects/${pid}/members`),
  addMember: (pid: number, email: string, role: Role) =>
    request<Member>('POST', `/api/projects/${pid}/members`, { email, role }),
  setRole: (pid: number, uid: number, role: Role) =>
    request<Member>('PATCH', `/api/projects/${pid}/members/${uid}`, { role }),
  removeMember: (pid: number, uid: number) => request<void>('DELETE', `/api/projects/${pid}/members/${uid}`),

  allFolders: (pid: number) => get<Folder[]>(`/api/projects/${pid}/folders?all=1`),
  listing: (pid: number, folder: number | null) => get<FolderListing>(`/api/projects/${pid}/documents?folder=${q(folder)}`),
  createFolder: (pid: number, name: string, parent_id: number | null) =>
    request<Folder>('POST', `/api/projects/${pid}/folders`, { name, parent_id }),
  renameFolder: (id: number, name: string) => request<Folder>('PATCH', `/api/folders/${id}`, { name }),
  moveFolder: (id: number, parent_id: number | null) => request<Folder>('PATCH', `/api/folders/${id}`, { parent_id }),
  deleteFolder: (id: number) => request<void>('DELETE', `/api/folders/${id}`),

  upload: (pid: number, folder: number | null, files: File[]) => {
    const fd = new FormData()
    if (folder != null) fd.append('folder_id', String(folder)) // must precede files
    files.forEach((f) => fd.append('file', f, f.name))
    return request<Document[]>('POST', `/api/projects/${pid}/documents`, fd)
  },
  document: (id: number) => get<Document>(`/api/documents/${id}`),
  pages: (id: number) => get<Page[]>(`/api/documents/${id}/pages`),
  fileUrl: (id: number, download = false) => `/api/documents/${id}/file${download ? '?download=1' : ''}`,
  renameDocument: (id: number, name: string) => request<Document>('PATCH', `/api/documents/${id}`, { name }),
  moveDocument: (id: number, folder_id: number | null) => request<Document>('PATCH', `/api/documents/${id}`, { folder_id }),
  deleteDocument: (id: number) => request<void>('DELETE', `/api/documents/${id}`),

  ask: (pid: number, question: string, folder_id: number | null) =>
    request<QuestionRecord>('POST', `/api/projects/${pid}/questions`, { question, folder_id }),
  history: (pid: number) => get<QuestionRecord[]>(`/api/projects/${pid}/questions`),

  tokens: () => get<AccessToken[]>('/api/tokens'),
  createToken: (name: string) => request<AccessToken>('POST', '/api/tokens', { name }),
  revokeToken: (id: number) => request<void>('DELETE', `/api/tokens/${id}`),
}
