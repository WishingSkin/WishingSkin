export interface PluginManifest {
  id: string
  version: string
  dependencies: string[]
  enabled: boolean
  error?: string
}

export function normalizePluginManifests(value: unknown): PluginManifest[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is Record<string, unknown> => !!item && typeof item === 'object').map(item => ({
    id: typeof item.id === 'string' ? item.id : '',
    version: typeof item.version === 'string' ? item.version : '',
    dependencies: Array.isArray(item.dependencies) ? item.dependencies.filter((dependency): dependency is string => typeof dependency === 'string') : [],
    enabled: item.enabled === true,
    error: typeof item.error === 'string' ? item.error : undefined,
  })).filter(plugin => plugin.id !== '')
}
