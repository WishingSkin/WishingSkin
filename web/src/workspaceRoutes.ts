export type AdminTab = 'users' | 'characters' | 'wardrobe' | 'resources' | 'reports' | 'events' | 'settings' | 'plugins'
export type SettingsTab = 'site' | 'appearance' | 'services' | 'operations'

export function normalizePath(path: string) {
  const normalized = path.replace(/\/+/g, '/') || '/'
  return normalized.length > 1 ? normalized.replace(/\/$/, '') : normalized
}

export function canonicalWorkspaceURL(path: string, search = '') {
  let pathname = normalizePath(path)
  const params = new URLSearchParams(search)
  if (pathname === '/user/materials') {
    const tab = params.get('tab')
    pathname = tab === 'upload' ? '/user/resources/upload' : '/user/resources'
    params.delete('tab')
    if (tab === 'skin' || tab === 'cape') params.set('kind', tab)
  }
  if (pathname === '/admin') pathname = '/admin/users'
  if (pathname === '/admin/textures') pathname = '/admin/resources'
  if (pathname === '/admin/settings') pathname = '/admin/settings/site'
  const query = params.toString()
  return pathname + (query ? `?${query}` : '')
}

export function isProtectedPath(path: string) {
  return path === '/user' || path.startsWith('/user/') || path === '/admin' || path.startsWith('/admin/')
}

export function resourcePath(view: 'browse' | 'upload', kind = '') {
  return view === 'upload' ? '/user/resources/upload' : '/user/resources' + (kind ? `?kind=${encodeURIComponent(kind)}` : '')
}

export function adminPath(tab: AdminTab) {
  return tab === 'settings' ? '/admin/settings/site' : `/admin/${tab}`
}

export function adminRoute(path: string) {
  const parts = normalizePath(path).split('/')
  const tab: AdminTab = (['users', 'characters', 'wardrobe', 'resources', 'reports', 'events', 'settings', 'plugins'] as const).find(tab => tab === parts[2]) || 'users'
  const settingsTab: SettingsTab = (['site', 'appearance', 'services', 'operations'] as const).find(tab => tab === parts[3]) || 'site'
  let pluginId = ''
  if (tab === 'plugins' && parts[3]) {
    try { pluginId = decodeURIComponent(parts[3]) } catch { /* An invalid URL opens the plugin catalog. */ }
  }
  return { tab, settingsTab, pluginId }
}

// Keep each workspace's heading and navigation still when only its panel changes.
export function navigationRegion(from: string, to: string) {
  if (from === to) return ''
  const within = (prefix: string) => [from, to].every(path => path === prefix || path.startsWith(prefix + '/'))
  if (within('/user/account')) return '[data-panel-region="account"]'
  if (within('/user/resources')) return '[data-panel-region="resources"]'
  if (within('/admin/settings')) return '[data-panel-region="settings"]'
  if (within('/admin/plugins')) return '[data-panel-region="plugins"]'
  if (within('/admin')) return '[data-panel-region="admin"]'
  return '.workspace-content, .public-home-page, .login-layout, .public-library-layout, .access-layout'
}
