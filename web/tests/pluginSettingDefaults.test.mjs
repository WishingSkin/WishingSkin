import assert from 'node:assert/strict'
import { test } from 'node:test'
import { normalizePluginManifests } from '../src/pluginModel.ts'

test('plugin setting metadata preserves default, description, and options', () => {
  const plugins = normalizePluginManifests([
    {
      id: 'wishingskin.ai-skin',
      version: '0.2.0',
      enabled: true,
      settings: [
        {
          key: 'daily_limit',
          label: 'Daily Limit',
          type: 'number',
          default: 5,
          description: 'Max generations per user per day',
        },
        {
          key: 'allow_demo',
          label: 'Allow Demo',
          type: 'boolean',
          default: false,
        },
        {
          key: 'mode',
          label: 'Mode',
          type: 'text',
          default: 'auto',
          options: [
            { value: 'auto', label: 'Auto' },
            { value: 'llm', label: 'LLM' },
            { value: 123, label: 'Ignored Invalid Option' },
          ],
        },
        {
          key: 'ignored_bad_default',
          label: 'Zero Kept When Valid',
          type: 'number',
          default: Number.NaN,
        },
      ],
    },
  ])

  assert.equal(plugins.length, 1)
  const settings = plugins[0].settings ?? []
  assert.deepEqual(settings[0], {
    key: 'daily_limit',
    label: 'Daily Limit',
    type: 'number',
    default: 5,
    description: 'Max generations per user per day',
  })
  assert.deepEqual(settings[1], {
    key: 'allow_demo',
    label: 'Allow Demo',
    type: 'boolean',
    default: false,
  })
  assert.deepEqual(settings[2], {
    key: 'mode',
    label: 'Mode',
    type: 'text',
    default: 'auto',
    options: [
      { value: 'auto', label: 'Auto' },
      { value: 'llm', label: 'LLM' },
    ],
  })
  assert.deepEqual(settings[3], {
    key: 'ignored_bad_default',
    label: 'Zero Kept When Valid',
    type: 'number',
  })
})
