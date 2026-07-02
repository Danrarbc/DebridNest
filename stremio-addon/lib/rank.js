const quality = require('./quality')
const seasonPacks = require('./seasonPacks')

const VIDEO_EXT = /\.(mp4|mkv|avi|webm|mov|m4v|wmv|flv|ts|m2ts)$/i

function parseSource(title) {
  const t = String(title || '').toLowerCase()
  if (/remux/u.test(t)) return 'Remux'
  if (/blu-?ray|bdrip|brrip/u.test(t)) return 'BluRay'
  if (/web-?dl/u.test(t)) return 'WEB-DL'
  if (/webrip/u.test(t)) return 'WEBRip'
  if (/hdtv/u.test(t)) return 'HDTV'
  return 'Unknown'
}

function splitFilterList(value) {
  if (value === undefined || value === null || value === '') {
    return []
  }
  if (Array.isArray(value)) {
    return value.flatMap((item) => splitFilterList(item))
  }
  return String(value)
    .split(/[;,|]/u)
    .map((part) => part.trim())
    .filter(Boolean)
}

function normalizeSourceLabel(value) {
  const normalized = String(value || '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '')
  if (!normalized || normalized === 'any' || normalized === 'all') {
    return null
  }
  if (normalized === 'remux') return 'Remux'
  if (['bluray', 'blu', 'bdrip', 'brrip', 'bd'].includes(normalized)) return 'BluRay'
  if (['webdl', 'webdownload'].includes(normalized)) return 'WEB-DL'
  if (['webrip', 'web'].includes(normalized)) return 'WEBRip'
  if (normalized === 'hdtv') return 'HDTV'
  if (normalized === 'unknown' || normalized === 'other') return 'Unknown'
  return null
}

function parseSourceFilter(value) {
  const labels = []
  for (const item of splitFilterList(value)) {
    const label = normalizeSourceLabel(item)
    if (label && !labels.includes(label)) {
      labels.push(label)
    }
  }
  return labels
}

function passesSourceFilter(source, sourceFilter) {
  const allowed = parseSourceFilter(sourceFilter)
  return !allowed.length || allowed.includes(source)
}

function matchesEpisode(title, meta) {
  if (meta.type !== 'series' || meta.season == null || meta.episode == null) {
    return true
  }
  if (meta.allowSeasonPack) {
    return true
  }
  const t = title.toLowerCase()
  const s = meta.season
  const e = meta.episode
  const sPad = pad2(s)
  const ePad = pad2(e)
  const patterns = [
    new RegExp(`\\bs${sPad}e${ePad}\\b`, 'i'),
    new RegExp(`\\bs${s}e${e}\\b`, 'i'),
    new RegExp(`\\b${s}x${e}\\b`, 'i'),
    new RegExp(`\\b${s}x${ePad}\\b`, 'i'),
    new RegExp(`\\b${sPad}x${ePad}\\b`, 'i'),
    new RegExp(`season\\s*${s}.*episode\\s*${e}`, 'i'),
    new RegExp(`\\b${ePad}\\b.*\\bs${sPad}\\b`, 'i'),
  ]
  return patterns.some((p) => p.test(t))
}

function pad2(n) {
  return String(n).padStart(2, '0')
}

function looksLikeVideoRelease(title) {
  const t = String(title || '').toLowerCase()
  if (VIDEO_EXT.test(t) || /\.(mp4|mkv|avi|webm|mov|m4v)/i.test(t)) {
    return true
  }
  return /2160p|4k|1080p|720p|480p|bluray|web-?dl|webrip|hdtv|sdtv|pdtv|remux|x264|x265|xvid|divx|hevc|dvdrip/u.test(t)
}

const TITLE_STOPWORDS = new Set([
  'a',
  'an',
  'and',
  'de',
  'la',
  'le',
  'les',
  'of',
  'the',
  'to',
])

function normalizeTitleWords(value) {
  return String(value || '')
    .toLowerCase()
    .replace(/&/g, ' and ')
    .replace(/\[.*?\]/g, ' ')
    .replace(/[^a-z0-9]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
}

function meaningfulTitleTokens(title) {
  return normalizeTitleWords(title)
    .split(' ')
    .filter(Boolean)
    .filter((token) => !TITLE_STOPWORDS.has(token))
    .filter((token) => !/^\d{4}$/u.test(token))
}

function matchesMovieTitle(title, meta) {
  if (meta.type !== 'movie' || !meta.title) {
    return true
  }

  const expected = normalizeTitleWords(meta.title)
  const actual = normalizeTitleWords(title)
  if (!expected || !actual) {
    return true
  }
  if (actual.includes(expected)) {
    return true
  }

  const tokens = meaningfulTitleTokens(meta.title)
  if (tokens.length === 0) {
    return true
  }

  // Very short one-word titles are too ambiguous to safely hard-filter.
  if (tokens.length === 1 && tokens[0].length <= 2) {
    return true
  }

  const actualWords = new Set(actual.split(' ').filter(Boolean))
  const matched = tokens.filter((token) => actualWords.has(token)).length
  if (matched === tokens.length) {
    return true
  }
  return tokens.length >= 3 && matched / tokens.length >= 0.7
}

function torrentRejectionReason(torrent, meta, qualityConfig = {}) {
  const isSeasonPack = torrent.seasonPack
    || seasonPacks.isSeasonPackForMeta(torrent.title, meta)
  const source = parseSource(torrent.title)

  if (!quality.passesQualityFilters(torrent, qualityConfig)) {
    return 'quality'
  }

  if (!passesSourceFilter(source, qualityConfig.sourceFilter)) {
    return 'source'
  }

  if (!isSeasonPack && !looksLikeVideoRelease(torrent.title)) {
    return 'notVideo'
  }

  if (!matchesMovieTitle(torrent.title, meta)) {
    return 'movieTitleMismatch'
  }

  const rankMeta = isSeasonPack
    ? { ...meta, allowSeasonPack: true }
    : meta

  if (!matchesEpisode(torrent.title, rankMeta)) {
    return 'episodeMismatch'
  }

  return null
}

function computeTorrentScore(torrent, meta, qualityConfig = {}) {
  const isSeasonPack = torrent.seasonPack
    || seasonPacks.isSeasonPackForMeta(torrent.title, meta)
  const parsedQuality = quality.parseQuality(torrent.title)
  const source = parseSource(torrent.title)
  let score = 0

  score += Math.min(torrent.seeders || 0, 500)
  score += parsedQuality.resolution * 2
  score += quality.hdrScoreAdjustment(torrent.title, qualityConfig.preferSdr)

  switch (source) {
    case 'Remux': score += 120; break
    case 'BluRay': score += 100; break
    case 'WEB-DL': score += 80; break
    case 'WEBRip': score += 60; break
    case 'HDTV': score += 40; break
    default: score += 20
  }

  if (meta.type === 'series' && /complete|season pack|s\d{2}\b(?!e)/i.test(torrent.title)) {
    if (meta.episode == null) {
      score += 50
    } else {
      score -= 30
    }
  }

  return score
}

function scoreTorrent(torrent, meta, qualityConfig = {}) {
  if (torrentRejectionReason(torrent, meta, qualityConfig)) {
    return -1
  }
  return computeTorrentScore(torrent, meta, qualityConfig)
}

function rankTorrents(torrents, meta, maxResults = 5, qualityConfig = {}) {
  return rankTorrentsDetailed(torrents, meta, maxResults, qualityConfig).entries
}

function rankTorrentsDetailed(torrents, meta, maxResults = 5, qualityConfig = {}) {
  const input = Array.isArray(torrents) ? torrents : []
  const rejected = {
    quality: 0,
    source: 0,
    notVideo: 0,
    episodeMismatch: 0,
    movieTitleMismatch: 0,
  }
  const entries = []

  for (const torrent of input) {
    const reason = torrentRejectionReason(torrent, meta, qualityConfig)
    if (reason) {
      rejected[reason] = (rejected[reason] || 0) + 1
      continue
    }
    entries.push({
      torrent,
      score: computeTorrentScore(torrent, meta, qualityConfig),
      quality: quality.parseQuality(torrent.title),
      source: parseSource(torrent.title),
    })
  }

  entries.sort((a, b) => b.score - a.score)
  const limited = entries.slice(0, maxResults)

  return {
    entries: limited,
    counts: {
      input: input.length,
      scored: entries.length,
      ranked: entries.length,
      returned: limited.length,
    },
    rejected,
  }
}

function countEpisodeMatches(torrents, meta, qualityConfig = {}) {
  return rankTorrents(torrents, meta, torrents.length, qualityConfig).length
}

const DISPLAY_TITLE_MAX = 80

function formatFileSize(bytes) {
  const size = Number(bytes || 0)
  if (size <= 0) {
    return null
  }
  const gb = size / (1024 ** 3)
  if (gb >= 100) {
    return `${Math.round(gb)} GB`
  }
  if (gb >= 10) {
    return `${gb.toFixed(1)} GB`
  }
  return `${gb.toFixed(2)} GB`
}

function formatDisplayTitle(title) {
  let text = String(title || 'Unknown').trim()
  text = text.replace(/^\[[^\]]+\]\s*/, '')
  if (text.length <= DISPLAY_TITLE_MAX) {
    return text
  }
  return `${text.slice(0, DISPLAY_TITLE_MAX - 3)}...`
}

function formatStreamMetadata(entry) {
  const { torrent } = entry
  const parts = [`Seeders: ${Number(torrent.seeders || 0)}`]
  if (Number(torrent.leechers || 0) > 0) {
    parts.push(`Leechers: ${Number(torrent.leechers || 0)}`)
  }
  const sizeLabel = formatFileSize(torrent.size)
  if (sizeLabel) {
    parts.push(`Size: ${sizeLabel}`)
  }
  const provider = torrent.indexer || null
  if (provider) {
    parts.push(`Provider: ${formatProviderName(provider)}`)
  }
  return parts.join(' | ')
}

function normalizeStreamOptions(options = {}) {
  if (typeof options === 'boolean') {
    return { cached: options }
  }
  return {
    cached: !!options.cached,
    showHealth: !!options.showHealth,
    debridStatus: options.debridStatus || '',
  }
}

function formatReadinessLabel(options = {}) {
  const { cached, debridStatus } = normalizeStreamOptions(options)
  if (cached) {
    return 'Ready now'
  }
  if (debridStatus === 'downloading') {
    return 'Downloading now'
  }
  return 'Starts download'
}

function streamHealth(entry) {
  const seeders = Number(entry?.torrent?.seeders || 0)
  const leechers = Number(entry?.torrent?.leechers || 0)
  let label = 'unknown'
  if (seeders >= 100) {
    label = 'excellent'
  } else if (seeders >= 30) {
    label = 'healthy'
  } else if (seeders >= 5) {
    label = 'fair'
  } else if (seeders > 0) {
    label = 'low'
  }
  return {
    label,
    seeders,
    leechers,
  }
}

function formatHealthLabel(entry) {
  const health = streamHealth(entry)
  const peers = [`${health.seeders} seeders`]
  if (health.leechers > 0) {
    peers.push(`${health.leechers} leechers`)
  }
  return `${health.label} (${peers.join(', ')})`
}

function formatStreamName(entry, cached = false) {
  const tags = quality.formatQualityTags(entry.torrent.title, entry.source)
  const prefix = formatReadinessLabel(cached)
  return [prefix, tags].filter(Boolean).join('\n')
}

function formatStreamDisplay(entry, options = {}) {
  const streamOptions = normalizeStreamOptions(options)
  return {
    name: formatStreamName(entry, streamOptions),
    title: formatDisplayTitle(entry.torrent.title),
    description: formatStreamMetadata(entry),
  }
}

function formatStreamLabel(entry, cached = false) {
  const display = formatStreamDisplay(entry, { cached })
  return display.name
}

function formatPlaceholderLabel(entry) {
  return formatStreamName(entry, false)
}

function applyCachePriority(ranked, availability) {
  return ranked.slice().sort((a, b) => {
    const aCached = isEntryCached(a, availability)
    const bCached = isEntryCached(b, availability)
    if (aCached !== bCached) {
      return aCached ? -1 : 1
    }
    return b.score - a.score
  })
}

function hasTorrentLink(entry) {
  const link = entry?.torrent?.torrentLink || entry?.torrent?.link
  if (!link || typeof link !== 'string') {
    return false
  }
  return !link.trim().toLowerCase().startsWith('magnet:')
}

function compareKnownLowerSize(a, b) {
  const aSize = Number(a.torrent.size || 0)
  const bSize = Number(b.torrent.size || 0)
  const aKnown = aSize > 0
  const bKnown = bSize > 0
  if (aKnown !== bKnown) {
    return aKnown ? -1 : 1
  }
  if (aKnown && aSize !== bSize) {
    return aSize - bSize
  }
  return 0
}

function compareFreshPlaceholderPriority(a, b) {
  const aHasTorrentLink = hasTorrentLink(a)
  const bHasTorrentLink = hasTorrentLink(b)
  if (aHasTorrentLink !== bHasTorrentLink) {
    return aHasTorrentLink ? -1 : 1
  }

  const seedersDelta = Number(b.torrent.seeders || 0) - Number(a.torrent.seeders || 0)
  if (seedersDelta !== 0) {
    return seedersDelta
  }

  const sizeDelta = compareKnownLowerSize(a, b)
  if (sizeDelta !== 0) {
    return sizeDelta
  }

  return b.score - a.score
}

function compareReadyPriority(a, b) {
  const scoreDelta = b.score - a.score
  if (scoreDelta !== 0) {
    return scoreDelta
  }

  const seedersDelta = Number(b.torrent.seeders || 0) - Number(a.torrent.seeders || 0)
  if (seedersDelta !== 0) {
    return seedersDelta
  }

  return compareKnownLowerSize(a, b)
}

function applyStreamListingPriority(ranked, availability) {
  return ranked.slice().sort((a, b) => {
    const aCached = isEntryCached(a, availability)
    const bCached = isEntryCached(b, availability)
    if (aCached !== bCached) {
      return aCached ? -1 : 1
    }
    if (!aCached && !bCached) {
      return compareFreshPlaceholderPriority(a, b)
    }
    return compareReadyPriority(a, b)
  })
}

function isEntryCached(entry, availability) {
  if (!entry.torrent.infoHash || !availability) {
    return false
  }
  const hash = entry.torrent.infoHash.toLowerCase()
  const item = availability[hash]
  if (!item) {
    return false
  }
  return Object.values(item).some((v) => Array.isArray(v) && v.length > 0)
}

function formatStreamFilename(title) {
  const cleaned = String(title || '').replace(/^\[[^\]]+\]\s*/, '').trim()
  const match = cleaned.match(/[^\s/\\]+\.(mkv|mp4|avi|webm|mov|m4v|ts|m2ts)/i)
  if (match) {
    return match[0]
  }
  const base = cleaned.slice(0, 120) || 'video'
  return /\.(mkv|mp4|avi|webm|mov|m4v|ts|m2ts)$/i.test(base) ? base : `${base}.mkv`
}

function formatStreamQualityLine(entry) {
  return quality.parseQuality(entry?.torrent?.title).label || 'Stream'
}

function parseCodecTag(title) {
  const text = String(title || '')
  if (/hevc|x265|h265/i.test(text)) {
    return 'hevc'
  }
  if (/x264|h264|avc/i.test(text)) {
    return 'x264'
  }
  if (/hdrip/i.test(text)) {
    return 'HDRip'
  }
  return null
}

function formatBingeGroup(entry, options = {}) {
  const qualityLine = formatStreamQualityLine(entry).toLowerCase()
  const parts = ['debridnest', qualityLine]
  const codec = parseCodecTag(entry?.torrent?.title)
  if (codec) {
    parts.push(codec.toLowerCase())
  }
  return parts.join('|')
}

function formatStremioStreamName(entry, options = {}) {
  const streamOptions = normalizeStreamOptions(options)
  const qualityLine = formatStreamQualityLine(entry)
  const badge = streamOptions.cached ? '[DN+] ' : '[DN download] '
  return `${badge}DebridNest\n${qualityLine}`
}

function formatStremioStreamTitle(entry, options = {}) {
  const { torrent } = entry
  const titleLine = formatDisplayTitle(torrent.title)
  const seeders = Number(torrent.seeders || 0)
  const sizeLabel = formatFileSize(torrent.size) || '?'
  const provider = formatProviderName(torrent.indexer) || 'Unknown'
  return `${titleLine}\n👤 ${seeders} 💾 ${sizeLabel} ⚙️ ${provider}`
}

function formatStatusDescription(options = {}) {
  const streamOptions = normalizeStreamOptions(options)
  if (streamOptions.cached) {
    return 'Status: ready now via DebridNest instant availability'
  }
  if (streamOptions.debridStatus) {
    return `Status: starts download (${streamOptions.debridStatus})`
  }
  return 'Status: starts download when selected'
}

function formatStremioStreamDescription(entry, options = {}) {
  const streamOptions = normalizeStreamOptions(options)
  const display = formatStreamDisplay(entry, streamOptions)
  const lines = [
    display.title,
    formatStatusDescription(streamOptions),
  ]
  if (streamOptions.showHealth) {
    lines.push(`Health: ${formatHealthLabel(entry)}`)
  }
  lines.push(display.description)
  return lines.filter(Boolean).join('\n')
}

function formatProviderName(provider) {
  const text = String(provider || '')
    .replace(/^\[[^\]]+\]\s*/, '')
    .trim()
  if (!text) {
    return ''
  }
  return text.length > 28 ? `${text.slice(0, 25)}...` : text
}

module.exports = {
  parseSource,
  parseSourceFilter,
  rankTorrents,
  rankTorrentsDetailed,
  countEpisodeMatches,
  formatStreamDisplay,
  formatStreamLabel,
  formatPlaceholderLabel,
  applyCachePriority,
  applyStreamListingPriority,
  isEntryCached,
  formatStreamFilename,
  formatBingeGroup,
  formatStremioStreamName,
  formatStremioStreamTitle,
  formatStremioStreamDescription,
  streamHealth,
  matchesMovieTitle,
  matchesEpisode,
  looksLikeVideoRelease,
}
