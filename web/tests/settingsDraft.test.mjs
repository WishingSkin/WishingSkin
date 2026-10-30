import assert from 'node:assert/strict'
import { test } from 'node:test'
import { settingsSnapshot, saveSettingsSections } from '../src/settingsDraft.ts'

test('reverting blank fields clears dirty state while list order is retained', () => {
  assert.equal(settingsSnapshot({ title: 'Test', copy: { en: {} } }), settingsSnapshot({ copy: { en: { key: '' } }, title: 'Test' }))
  assert.notEqual(settingsSnapshot({ blocks: ['a', 'b'] }), settingsSnapshot({ blocks: ['b', 'a'] }))
})

test('save all retries only failed sections and keeps later changes', async () => {
  const calls = []
  let fail = true
  function section(id) { return { dirty: true, busy: false, async save() { calls.push(id); if (id === 2 && fail) return false; this.dirty = false; return true } } }
  const sections = [section(1), section(2), section(3)]
  assert.equal(await saveSettingsSections(sections), false)
  assert.deepEqual(calls, [1, 2, 3])
  assert.deepEqual(sections.map(s => s.dirty), [false, true, false])
  fail = false
  assert.equal(await saveSettingsSections(sections), true)
  assert.deepEqual(calls, [1, 2, 3, 2])
})
