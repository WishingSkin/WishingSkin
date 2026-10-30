import assert from 'node:assert/strict'
import { test } from 'node:test'
import { interfaceAppearance, setInterfaceAppearance, applyInterfaceColors, interfaceText, cloneAdvanced } from '../src/interfaceAppearance.ts'

test('appearance clones server maps and restores colors when switching modes', () => {
  const source = { light: { heading: '#123456', panel: 'url(bad)' }, dark: { heading: '#abcdef' }, copy: { 登录: '进入' }, icons: { characters: '◇' } }
  setInterfaceAppearance(source)
  source.copy.登录 = 'changed'
  assert.equal(interfaceAppearance.value.localizedCopy['zh-CN'].登录, '进入')
  assert.equal(interfaceAppearance.value.localizedCopy.en.登录, '进入')
  const properties = new Map()
  globalThis.document = { documentElement: { style: { setProperty: (k, v) => properties.set(k, v), removeProperty: k => properties.delete(k) } } }
  try {
    applyInterfaceColors(false)
    assert.equal(properties.get('--heading'), '#123456')
    assert.equal(properties.has('--panel'), false)
    applyInterfaceColors(true)
    assert.equal(properties.get('--heading'), '#abcdef')
    setInterfaceAppearance()
    applyInterfaceColors(false)
    assert.equal(properties.size, 0)
    assert.deepEqual(interfaceAppearance.value, cloneAdvanced())
  } finally { delete globalThis.document }
})

test('localized copy remains isolated and clears to the selected language default', () => {
  const source = { localizedCopy: { 'zh-CN': { 登录: '进入' }, en: { 登录: 'Sign in' } } }
  setInterfaceAppearance(source)
  source.localizedCopy.en.登录 = 'changed'
  assert.equal(interfaceText('登录', 'zh-CN', '登录'), '进入')
  assert.equal(interfaceText('登录', 'en', 'Log in'), 'Sign in')
  const draft = cloneAdvanced(interfaceAppearance.value)
  delete draft.localizedCopy.en.登录
  setInterfaceAppearance(draft)
  assert.equal(interfaceText('登录', 'zh-CN', '登录'), '进入')
  assert.equal(interfaceText('登录', 'en', 'Log in'), 'Log in')
  setInterfaceAppearance()
})
