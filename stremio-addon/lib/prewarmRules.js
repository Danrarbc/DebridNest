const MODES = new Set(['off', 'cached', 'all'])

function clampNonNegativeInt(value, fallback = 0, max = 10) {
  if (value === undefined || value === null || value === '') {
    return Math.max(0, Math.min(max, Number(fallback) || 0))
  }
  const parsed = Number(value)
  if (!Number.isFinite(parsed)) {
    return Math.max(0, Math.min(max, Number(fallback) || 0))
  }
  return Math.max(0, Math.min(max, Math.floor(parsed)))
}

function normalizeToken(value) {
  return String(value || '').trim().toLowerCase()
}

function parseCsv(value) {
  if (Array.isArray(value)) {
    return value.map(normalizeToken).filter(Boolean)
  }
  return String(value || '')
    .split(',')
    .map(normalizeToken)
    .filter(Boolean)
}

function resolveMode(value, fallback = 'off') {
  const normalized = normalizeToken(value || fallback)
  return MODES.has(normalized) ? normalized : 'off'
}

function resolveConfig(userConfig = {}, defaults = {}) {
  return {
    prewarmCount: clampNonNegativeInt(
      userConfig.prewarmCount,
      defaults.count ?? defaults.prewarmCount ?? 0,
    ),
    prewarmMode: resolveMode(userConfig.prewarmMode, defaults.mode ?? defaults.prewarmMode ?? 'off'),
    prewarmIndexers: parseCsv(userConfig.prewarmIndexers ?? defaults.indexers ?? defaults.prewarmIndexers),
  }
}

function listMatches(value, allowlist) {
  if (!allowlist || allowlist.length === 0) {
    return true
  }
  const normalized = normalizeToken(value)
  if (!normalized) {
    return false
  }
  return allowlist.some((allowed) => normalized.includes(allowed) || allowed.includes(normalized))
}

function shouldPrewarm(entry, cached, config = {}) {
  if (!entry || !config.prewarmCount || config.prewarmMode === 'off') {
    return false
  }
  if (config.prewarmMode === 'cached' && !cached) {
    return false
  }
  return listMatches(entry.torrent?.indexer, config.prewarmIndexers)
}

module.exports = {
  clampNonNegativeInt,
  parseCsv,
  resolveConfig,
  shouldPrewarm,
}
