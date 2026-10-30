// Ignore empty optional fields and object key order, but preserve array order.
export function settingsSnapshot(value: unknown): string {
  return JSON.stringify(value, (_, item) => {
    if (item && typeof item === 'object' && !Array.isArray(item)) {
      return Object.fromEntries(Object.entries(item).filter(([, v]) => v !== '' && v !== undefined).sort(([a], [b]) => a.localeCompare(b)))
    }
    return item
  })
}

export interface SettingsSection {
  dirty: boolean
  busy: boolean
  save: () => Promise<boolean>
}

export async function saveSettingsSections(sections: SettingsSection[]) {
  let failed = false
  for (const section of sections) {
    if (section.dirty && !await section.save()) failed = true
  }
  return !failed
}
