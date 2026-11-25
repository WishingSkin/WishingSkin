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

export interface PluginSettingOption {
  value: string
  label: string
}

export interface PluginSettingField {
  key: string
  label: string
  type: string
  default?: string | number | boolean
  description?: string
  options?: PluginSettingOption[]
}

export interface PluginManifest {
  name?: string
  description?: string
  origin?: string
  external?: boolean
  signedOfficial?: boolean
  hooks?: string[]
  userFrontends?: UserPluginFrontend[]
  settings?: PluginSettingField[]
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

function normalizeSettings(value: unknown): PluginSettingField[] {
  if (!Array.isArray(value)) return []
  const out: PluginSettingField[] = []
  for (const item of value) {
    if (!item || typeof item !== 'object') continue
    const raw = item as Record<string, unknown>
    if (typeof raw.key !== 'string' || typeof raw.label !== 'string' || typeof raw.type !== 'string') continue
    const field: PluginSettingField = {
      key: raw.key,
      label: raw.label,
      type: raw.type,
    }
    if (typeof raw.description === 'string' && raw.description) {
      field.description = raw.description
    }
    if (typeof raw.default === 'boolean' || (typeof raw.default === 'number' && Number.isFinite(raw.default)) || typeof raw.default === 'string') {
      field.default = raw.default
    }
    if (Array.isArray(raw.options)) {
      const options = raw.options
        .filter((opt): opt is Record<string, unknown> => !!opt && typeof opt === 'object' && typeof opt.value === 'string' && typeof opt.label === 'string')
        .map(opt => ({ value: opt.value as string, label: opt.label as string }))
      if (options.length > 0) {
        field.options = options
      }
    }
    out.push(field)
  }
  return out
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
    settings: normalizeSettings(item.settings),
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
