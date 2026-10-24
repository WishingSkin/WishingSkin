import assert from 'node:assert/strict'
import { test } from 'node:test'
import { normalizePluginManifests } from '../src/pluginModel.ts'

test('plugin manifests tolerate omitted dependency arrays', () => {
  const plugins = normalizePluginManifests([
    { id: 'wishingskin.points', version: '1.0.0', dependencies: null, enabled: true },
    { id: 'wishingskin.invitations', version: '1.1.0', enabled: false },
  ])

  assert.deepEqual(plugins.map(plugin => plugin.dependencies), [[], []])
  assert.deepEqual(plugins.map(plugin => plugin.id), ['wishingskin.points', 'wishingskin.invitations'])
})
