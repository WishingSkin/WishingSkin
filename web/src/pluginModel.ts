export interface PluginManifest {
  name?: string
  description?: string
  origin?: string
  settings?: { key: string; label: string; type: string }[]
  id: string
  version: string
  dependencies: string[]
  enabled: boolean
  error?: string
}

export function normalizePluginManifests(value: unknown): PluginManifest[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is Record<string, unknown> => !!item && typeof item === 'object').map(item => ({
    name: typeof item.name === 'string' ? item.name : undefined,
    description: typeof item.description === 'string' ? item.description : undefined,
    origin: item.origin === 'community' ? 'community' : 'official',
    settings: Array.isArray(item.settings) ? item.settings.filter((f): f is { key: string; label: string; type: string } => !!f && typeof f === 'object' && typeof f.key === 'string' && typeof f.label === 'string' && typeof f.type === 'string') : [],
    id: typeof item.id === 'string' ? item.id : '',
    version: typeof item.version === 'string' ? item.version : '',
    dependencies: Array.isArray(item.dependencies) ? item.dependencies.filter((dependency): dependency is string => typeof dependency === 'string') : [],
    enabled: item.enabled === true,
    error: typeof item.error === 'string' ? item.error : undefined,
  })).filter(plugin => plugin.id !== '')
}
