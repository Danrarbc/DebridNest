const CINEMETA_BASES = [
  process.env.CINEMETA_BASE || 'https://cinemeta-live.strem.io',
  'https://v3-cinemeta.strem.io',
]
const CINEMETA_CACHE_TTL_MS = Number(process.env.CINEMETA_CACHE_TTL_MS || 6 * 60 * 60 * 1000)
const metaCache = new Map()

function hasUsableMeta(meta) {
  return !!(meta && (meta.name || meta.imdb_id))
}

function parseStremioId(id) {
  if (!id || !id.startsWith('tt')) {
    return null
  }
  const parts = id.split(':')
  const imdbId = parts[0]
  const season = parts.length > 1 ? Number.parseInt(parts[1], 10) : null
  const episode = parts.length > 2 ? Number.parseInt(parts[2], 10) : null
  return {
    imdbId,
    season: Number.isFinite(season) ? season : null,
    episode: Number.isFinite(episode) ? episode : null,
  }
}

async function fetchMeta(type, imdbId) {
  const cacheKey = `${type}:${imdbId}`
  if (CINEMETA_CACHE_TTL_MS > 0) {
    const cached = metaCache.get(cacheKey)
    if (cached && cached.expiresAt > Date.now()) {
      return cached.promise
    }
    if (cached) {
      metaCache.delete(cacheKey)
    }
  }

  const path = type === 'series'
    ? `/meta/series/${imdbId}.json`
    : `/meta/movie/${imdbId}.json`
  const promise = (async () => {
    let lastError = null
    for (const base of CINEMETA_BASES) {
      try {
        const res = await fetch(`${base}${path}`, { redirect: 'follow' })
        if (!res.ok) {
          lastError = new Error(`Cinemeta ${res.status}`)
          continue
        }
        const data = await res.json()
        if (hasUsableMeta(data?.meta)) {
          return data.meta
        }
      } catch (err) {
        lastError = err
      }
    }
    if (lastError) {
      throw lastError
    }
    return null
  })().catch((err) => {
    metaCache.delete(cacheKey)
    throw err
  })

  if (CINEMETA_CACHE_TTL_MS > 0) {
    metaCache.set(cacheKey, {
      promise,
      expiresAt: Date.now() + CINEMETA_CACHE_TTL_MS,
    })
  }
  return promise
}

async function resolveMetadata(type, id) {
  const parsed = parseStremioId(id)
  if (!parsed) {
    return null
  }

  let meta = null
  try {
    meta = await fetchMeta(type, parsed.imdbId)
  } catch (err) {
    console.warn(`[cinemeta] lookup failed for ${type} ${parsed.imdbId}: ${err.message}`)
  }

  if (!hasUsableMeta(meta)) {
    console.warn(`[cinemeta] No metadata for ${type} ${parsed.imdbId}; continuing with IMDB id only`)
    return {
      imdbId: parsed.imdbId,
      type,
      title: null,
      year: null,
      season: parsed.season,
      episode: parsed.episode,
    }
  }

  return {
    imdbId: parsed.imdbId,
    type,
    title: meta.name,
    year: meta.year,
    season: parsed.season,
    episode: parsed.episode,
  }
}

module.exports = {
  parseStremioId,
  resolveMetadata,
}
