const test = require('node:test')
const assert = require('node:assert/strict')

const prewarmRules = require('../lib/prewarmRules')

test('resolveConfig clamps prewarm count and parses indexers', () => {
  const config = prewarmRules.resolveConfig({
    prewarmCount: '99',
    prewarmMode: 'all',
    prewarmIndexers: 'LimeTorrents, 1337x ,,',
  }, { count: 1, mode: 'cached' })

  assert.equal(config.prewarmCount, 10)
  assert.equal(config.prewarmMode, 'all')
  assert.deepEqual(config.prewarmIndexers, ['limetorrents', '1337x'])
})

test('resolveConfig falls back safely for bad values', () => {
  const config = prewarmRules.resolveConfig({
    prewarmCount: 'not-a-number',
    prewarmMode: 'everything',
  }, { count: 2, mode: 'cached', indexers: 'magnetz' })

  assert.equal(config.prewarmCount, 2)
  assert.equal(config.prewarmMode, 'off')
  assert.deepEqual(config.prewarmIndexers, ['magnetz'])
})

test('shouldPrewarm obeys mode, cache state, and indexer allowlist', () => {
  const entry = { torrent: { indexer: 'LimeTorrents' } }

  assert.equal(prewarmRules.shouldPrewarm(entry, false, {
    prewarmCount: 1,
    prewarmMode: 'cached',
    prewarmIndexers: [],
  }), false)

  assert.equal(prewarmRules.shouldPrewarm(entry, true, {
    prewarmCount: 1,
    prewarmMode: 'cached',
    prewarmIndexers: ['lime'],
  }), true)

  assert.equal(prewarmRules.shouldPrewarm(entry, false, {
    prewarmCount: 1,
    prewarmMode: 'all',
    prewarmIndexers: ['1337x'],
  }), false)
})
