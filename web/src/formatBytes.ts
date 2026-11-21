export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let value = bytes
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index++
  }
  const decimals = index === 0 ? 0 : value >= 100 ? 0 : value >= 10 ? 1 : 2
  return `${Number(value.toFixed(decimals))} ${units[index]}`
}

export function quotaPercent(used: number, limit: number): number {
  if (!Number.isFinite(used) || !Number.isFinite(limit) || limit <= 0 || used <= 0) return 0
  return Math.min(100, Math.max(0, Math.round((used / limit) * 100)))
}

export function authlibInjectorDragUrl(apiRoot: string): string {
  const trimmed = apiRoot.trim()
  if (!trimmed) return ''
  return `authlib-injector:yggdrasil-server:${encodeURIComponent(trimmed)}`
}
