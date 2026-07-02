const test = require('node:test')
const assert = require('node:assert/strict')

const rank = require('../lib/rank')

test('Stremio labels match Torrentio-style name and title layout', () => {
  const entry = {
    torrent: {
      title: '[1337x] Backrooms.2026.1080p.WEB-DL.mkv',
      indexer: '1337x',
      seeders: 123,
      leechers: 4,
      size: 2 * 1024 ** 3,
    },
    source: 'WEB-DL',
    score: 100,
  }

  const readyName = rank.formatStremioStreamName(entry, { cached: true })
  assert.equal(readyName, '[DN+] DebridNest\n1080p')

  const freshName = rank.formatStremioStreamName(entry, { cached: false })
  assert.equal(freshName, '[DN download] DebridNest\n1080p')

  const title = rank.formatStremioStreamTitle(entry, {
    cached: true,
    showHealth: true,
  })
  assert.equal(
    title,
    'Backrooms.2026.1080p.WEB-DL.mkv\n👤 123 💾 2.00 GB ⚙️ 1337x',
  )
})

test('Stremio binge groups follow torrentio-style quality and codec tags', () => {
  const entry = {
    torrent: {
      title: 'Backrooms.2026.2160p.BluRay.x265.mkv',
    },
    source: 'BluRay',
  }

  assert.equal(rank.formatBingeGroup(entry, { cached: true }), 'debridnest|4k|hevc')
  assert.equal(rank.formatBingeGroup(entry, { cached: false }), 'debridnest|4k|hevc')
})
