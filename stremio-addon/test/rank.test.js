const test = require('node:test')
const assert = require('node:assert/strict')

const rank = require('../lib/rank')

test('detailed ranking reports quality, title, and non-video rejections', () => {
  const result = rank.rankTorrentsDetailed(
    [
      {
        title: 'Backrooms.2026.1080p.WEB-DL.mkv',
        seeders: 50,
        size: 2 * 1024 ** 3,
      },
      {
        title: 'Backrooms.2026.2160p.WEB-DL.mkv',
        seeders: 100,
        size: 4 * 1024 ** 3,
      },
      {
        title: 'Different.Movie.2026.1080p.WEB-DL.mkv',
        seeders: 100,
        size: 2 * 1024 ** 3,
      },
      {
        title: 'Backrooms soundtrack',
        seeders: 100,
        size: 100 * 1024 ** 2,
      },
    ],
    {
      type: 'movie',
      title: 'Backrooms',
      year: 2026,
    },
    10,
    {
      maxResolution: 1080,
      maxFileSizeGb: 0,
      preferSdr: false,
    },
  )

  assert.equal(result.entries.length, 1)
  assert.equal(result.entries[0].torrent.title, 'Backrooms.2026.1080p.WEB-DL.mkv')
  assert.deepEqual(result.rejected, {
    quality: 1,
    source: 0,
    notVideo: 1,
    episodeMismatch: 0,
    movieTitleMismatch: 1,
  })
  assert.equal(result.counts.input, 4)
  assert.equal(result.counts.scored, 1)
  assert.equal(result.counts.returned, 1)
})

test('ranking supports quality and source allowlists from config URLs', () => {
  const result = rank.rankTorrentsDetailed(
    [
      {
        title: 'Backrooms.2026.2160p.Remux.mkv',
        seeders: 100,
        size: 30 * 1024 ** 3,
      },
      {
        title: 'Backrooms.2026.1080p.WEB-DL.mkv',
        seeders: 80,
        size: 4 * 1024 ** 3,
      },
      {
        title: 'Backrooms.2026.720p.HDTV.mkv',
        seeders: 200,
        size: 1 * 1024 ** 3,
      },
    ],
    {
      type: 'movie',
      title: 'Backrooms',
      year: 2026,
    },
    10,
    {
      maxResolution: 0,
      maxFileSizeGb: 0,
      preferSdr: false,
      qualityFilter: ['1080p', '720p'],
      sourceFilter: 'WEB-DL,BluRay',
    },
  )

  assert.equal(result.entries.length, 1)
  assert.equal(result.entries[0].torrent.title, 'Backrooms.2026.1080p.WEB-DL.mkv')
  assert.equal(result.rejected.quality, 1)
  assert.equal(result.rejected.source, 1)
})

test('stream listing priority groups ready-now before starts-download', () => {
  const entries = [
    {
      torrent: {
        title: 'Fresh.1080p.WEB-DL.mkv',
        infoHash: 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',
        seeders: 500,
        size: 2 * 1024 ** 3,
      },
      score: 5000,
    },
    {
      torrent: {
        title: 'Ready.720p.WEB-DL.mkv',
        infoHash: 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
        seeders: 5,
        size: 1 * 1024 ** 3,
      },
      score: 10,
    },
  ]

  const ordered = rank.applyStreamListingPriority(entries, {
    aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: {
      'real-debrid.com': [{ filename: 'Ready.720p.WEB-DL.mkv' }],
    },
  })

  assert.equal(ordered[0].torrent.title, 'Ready.720p.WEB-DL.mkv')
  assert.equal(ordered[1].torrent.title, 'Fresh.1080p.WEB-DL.mkv')
})

test('looksLikeVideoRelease accepts legacy PDTV and SDTV episode naming', () => {
  assert.equal(rank.looksLikeVideoRelease('the It Crowd s02e05 Ws Pdtv xvid-river'), true)
  assert.equal(rank.looksLikeVideoRelease('The IT Crowd S02E05 SDTV XviD DargeN'), true)
})

test('IT Crowd S02E5 torrents pass ranking filters', () => {
  const result = rank.rankTorrentsDetailed(
    [
      { title: 'the It Crowd s02e05 Ws Pdtv xvid-river', seeders: 10, size: 350 * 1024 ** 2 },
      { title: 'The IT Crowd S02E05 SDTV XviD DargeN', seeders: 8, size: 350 * 1024 ** 2 },
      { title: 'The IT Crowd S01E06 Aunt Irma Visits FiNAL PL 1080p WEB-DL', seeders: 50, size: 1024 ** 3 },
    ],
    {
      type: 'series',
      title: 'The IT Crowd',
      season: 2,
      episode: 5,
    },
    10,
    {
      maxResolution: 0,
      maxFileSizeGb: 0,
      preferSdr: false,
    },
  )

  assert.equal(result.entries.length, 2)
  assert.equal(result.rejected.episodeMismatch, 1)
  assert.equal(result.rejected.notVideo, 0)
})
