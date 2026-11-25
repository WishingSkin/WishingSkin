export interface UserPluginFrontend {
  id: string
  label: string
  description?: string
  slot: 'user.tools' | 'texture.action'
  path?: string
  kinds?: ('skin' | 'cape')[]
}

export interface UserPluginEntry {
  id: string
  name?: string
  description?: string
  version: string
  signedOfficial: boolean
  frontends: UserPluginFrontend[]
}

export interface PluginManifest {
  name?: string
  description?: string
  origin?: string
  external?: boolean
  signedOfficial?: boolean
  hooks?: string[]
  userFrontends?: UserPluginFrontend[]
  settings?: { key: string; label: string; type: string }[]
  id: string
  version: string
  dependencies: string[]
  enabled: boolean
  error?: string
}

function normalizeUserFrontends(value: unknown): UserPluginFrontend[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is Record<string, unknown> => !!item && typeof item === 'object').map((item): UserPluginFrontend => ({
    id: typeof item.id === 'string' ? item.id : '',
    label: typeof item.label === 'string' ? item.label : '',
    description: typeof item.description === 'string' ? item.description : undefined,
    slot: item.slot === 'texture.action' ? 'texture.action' : 'user.tools',
    path: typeof item.path === 'string' ? item.path : undefined,
    kinds: Array.isArray(item.kinds)
      ? item.kinds.filter((kind): kind is 'skin' | 'cape' => kind === 'skin' || kind === 'cape')
      : undefined,
  })).filter(frontend => frontend.id !== '' && frontend.label !== '')
}

export function normalizePluginManifests(value: unknown): PluginManifest[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is Record<string, unknown> => !!item && typeof item === 'object').map(item => ({
    name: typeof item.name === 'string' ? item.name : undefined,
    description: typeof item.description === 'string' ? item.description : undefined,
    origin: item.origin === 'community' ? 'community' : 'official',
    external: item.external === true,
    signedOfficial: item.signedOfficial === true,
    hooks: Array.isArray(item.hooks) ? item.hooks.filter((h): h is string => typeof h === 'string') : [],
    userFrontends: normalizeUserFrontends(item.userFrontends),
    settings: Array.isArray(item.settings) ? item.settings.filter((f): f is { key: string; label: string; type: string } => !!f && typeof f === 'object' && typeof f.key === 'string' && typeof f.label === 'string' && typeof f.type === 'string') : [],
    id: typeof item.id === 'string' ? item.id : '',
    version: typeof item.version === 'string' ? item.version : '',
    dependencies: Array.isArray(item.dependencies) ? item.dependencies.filter((dependency): dependency is string => typeof dependency === 'string') : [],
    enabled: item.enabled === true,
    error: typeof item.error === 'string' ? item.error : undefined,
  })).filter(plugin => plugin.id !== '')
}

export function normalizeUserPlugins(value: unknown): UserPluginEntry[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is Record<string, unknown> => !!item && typeof item === 'object').map(item => ({
    id: typeof item.id === 'string' ? item.id : '',
    name: typeof item.name === 'string' ? item.name : undefined,
    description: typeof item.description === 'string' ? item.description : undefined,
    version: typeof item.version === 'string' ? item.version : '',
    signedOfficial: item.signedOfficial === true,
    frontends: normalizeUserFrontends(item.frontends),
  })).filter(plugin => plugin.id !== '' && plugin.frontends.length > 0)
}
