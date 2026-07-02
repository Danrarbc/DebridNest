import { type FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { fetchConfig, getToken, type Config } from '../api';
import CopyButton from '../components/CopyButton';
import Icon from '../components/Icon';
import Toggle from '../components/Toggle';
import { useToast } from '../components/Toast';
import { usePolling } from '../hooks/usePolling';

interface SetupForm {
  addonBaseUrl: string;
  manifestLabel: string;
  apiUrl: string;
  apiToken: string;
  jackettUrl: string;
  jackettApiKey: string;
  preset: string;
  maxResults: string;
  maxResolution: string;
  maxFileSizeGb: string;
  qualityFilter: string;
  sourceFilter: string;
  preferSdr: boolean;
  showStreamHealth: boolean;
  prewarmCount: string;
  prewarmMode: string;
  prewarmIndexers: string;
}

interface AddonDiagnostics {
  ok: boolean;
  manifest?: {
    ok: boolean;
    id: string;
    name: string;
    version: string;
  };
  debridnest?: {
    ok: boolean;
    username?: string;
    error?: string;
  };
  jackett?: {
    ok: boolean;
    configuredIndexers?: number;
    totalIndexers?: number;
    error?: string;
  };
  prewarm?: {
    count: number;
    mode: string;
    indexers: string[];
  };
  hints?: string[];
  error?: string;
}

const DOCKER_API_URL = 'http://debridnest:8080/rest/1.0';
const DOCKER_JACKETT_URL = 'http://jackett:9117';

function stripTrailingSlash(value: string): string {
  return value.trim().replace(/\/+$/, '');
}

function isPrivateHost(hostname: string): boolean {
  return (
    hostname === 'localhost' ||
    hostname === '127.0.0.1' ||
    hostname.startsWith('10.') ||
    hostname.startsWith('192.168.') ||
    /^172\.(1[6-9]|2\d|3[0-1])\./.test(hostname)
  );
}

function withPort(origin: string, port: string): string {
  try {
    const url = new URL(origin);
    url.port = port;
    return stripTrailingSlash(url.toString());
  } catch {
    return '';
  }
}

function inferAddonBase(publicUrl = ''): string {
  const current = window.location.origin;
  const sources = [current, publicUrl].filter(Boolean);

  for (const source of sources) {
    try {
      const url = new URL(source);
      if (url.hostname.startsWith('debridnest.')) {
        url.hostname = `stremio.${url.hostname.slice('debridnest.'.length)}`;
        return stripTrailingSlash(url.toString());
      }
      if (url.hostname.startsWith('dashboard.')) {
        url.hostname = `stremio.${url.hostname.slice('dashboard.'.length)}`;
        return stripTrailingSlash(url.toString());
      }
      if (url.port === '8080' || isPrivateHost(url.hostname)) {
        return withPort(url.origin, '7001');
      }
    } catch {
      // try the next source
    }
  }

  return 'http://127.0.0.1:7001';
}

function defaultForm(): SetupForm {
  return {
    addonBaseUrl: '',
    manifestLabel: '',
    apiUrl: DOCKER_API_URL,
    apiToken: getToken() ?? '',
    jackettUrl: DOCKER_JACKETT_URL,
    jackettApiKey: '',
    preset: 'Default',
    maxResults: '20',
    maxResolution: 'Any',
    maxFileSizeGb: '0',
    qualityFilter: '',
    sourceFilter: '',
    preferSdr: false,
    showStreamHealth: false,
    prewarmCount: '1',
    prewarmMode: 'all',
    prewarmIndexers: '',
  };
}

function addConfigValue(config: Record<string, string>, key: string, value: string, defaultValue = '') {
  const trimmed = value.trim();
  if (!trimmed || trimmed === defaultValue) return;
  config[key] = trimmed;
}

function buildInstallConfig(form: SetupForm): Record<string, string> {
  const config: Record<string, string> = {};

  addConfigValue(config, 'manifestLabel', form.manifestLabel);
  addConfigValue(config, 'apiUrl', form.apiUrl, DOCKER_API_URL);
  addConfigValue(config, 'apiToken', form.apiToken);
  addConfigValue(config, 'jackettUrl', form.jackettUrl, DOCKER_JACKETT_URL);
  addConfigValue(config, 'jackettApiKey', form.jackettApiKey);
  addConfigValue(config, 'preset', form.preset, 'Default');
  addConfigValue(config, 'maxResults', form.maxResults, '20');
  addConfigValue(config, 'maxResolution', form.maxResolution, 'Any');
  addConfigValue(config, 'maxFileSizeGb', form.maxFileSizeGb, '0');
  addConfigValue(config, 'qualityFilter', form.qualityFilter);
  addConfigValue(config, 'sourceFilter', form.sourceFilter);
  addConfigValue(config, 'prewarmCount', form.prewarmCount, '0');
  addConfigValue(config, 'prewarmMode', form.prewarmMode, 'off');
  addConfigValue(config, 'prewarmIndexers', form.prewarmIndexers);

  if (form.preferSdr) config.preferSdr = 'true';
  if (form.showStreamHealth) config.showStreamHealth = 'true';

  return config;
}

function configuredPath(config: Record<string, string>, suffix: string): string {
  return `/${encodeURIComponent(JSON.stringify(config))}${suffix}`;
}

function stremioInstallUrl(manifestUrl: string): string {
  try {
    const url = new URL(manifestUrl);
    return `stremio://${url.host}${url.pathname}${url.search}`;
  } catch {
    return '';
  }
}

function statusClass(ok: boolean | undefined): string {
  if (ok === true) return 'pill pill-live';
  if (ok === false) return 'pill pill-warning';
  return 'pill pill-muted';
}

function CheckRow({
  label,
  ok,
  detail,
}: {
  label: string;
  ok?: boolean;
  detail: string;
}) {
  return (
    <div className="stremio-check-row">
      <span className={statusClass(ok)}>{ok == null ? 'Pending' : ok ? 'OK' : 'Check'}</span>
      <span className="stremio-check-text">
        <strong>{label}</strong>
        <span>{detail}</span>
      </span>
    </div>
  );
}

function UrlBox({ label, url }: { label: string; url: string }) {
  return (
    <div className="url-row">
      <div className="url-row-text">
        <span className="config-label">{label}</span>
        <code className="url-value">{url || '—'}</code>
      </div>
      <CopyButton value={url} label="Copy" />
    </div>
  );
}

export default function StremioSetup() {
  const { toast } = useToast();
  const [form, setForm] = useState<SetupForm>(() => defaultForm());
  const [dirtyBase, setDirtyBase] = useState(false);
  const [manifestResult, setManifestResult] = useState<{ id?: string; name?: string } | null>(null);
  const [diagnostics, setDiagnostics] = useState<AddonDiagnostics | null>(null);
  const [busy, setBusy] = useState<'manifest' | 'diagnostics' | null>(null);

  const configLoader = useCallback(() => fetchConfig(), []);
  const { data: config, error, loading, refresh } = usePolling<Config>(configLoader, {
    intervalMs: 30000,
  });

  useEffect(() => {
    if (!dirtyBase && config?.publicUrl) {
      setForm((current) => ({
        ...current,
        addonBaseUrl: current.addonBaseUrl || inferAddonBase(config.publicUrl),
      }));
    }
  }, [config?.publicUrl, dirtyBase]);

  const installConfig = useMemo(() => buildInstallConfig(form), [form]);
  const encodedConfigPath = useMemo(() => configuredPath(installConfig, '/manifest.json'), [installConfig]);
  const manifestUrl = form.addonBaseUrl
    ? `${stripTrailingSlash(form.addonBaseUrl)}${encodedConfigPath}`
    : '';
  const diagnosticsUrl = form.addonBaseUrl
    ? `${stripTrailingSlash(form.addonBaseUrl)}${configuredPath(installConfig, '/diagnostics')}`
    : '';
  const installUrl = manifestUrl ? stremioInstallUrl(manifestUrl) : '';

  const localWarnings = useMemo(() => {
    const warnings: string[] = [];
    try {
      const addonUrl = new URL(form.addonBaseUrl);
      const dashboardUrl = new URL(window.location.origin);
      if (isPrivateHost(addonUrl.hostname) && !isPrivateHost(dashboardUrl.hostname)) {
        warnings.push('Addon URL is private/local while this dashboard is remote.');
      }
      if (addonUrl.protocol !== 'https:' && !isPrivateHost(addonUrl.hostname)) {
        warnings.push('Remote Stremio installs are more reliable over HTTPS.');
      }
    } catch {
      warnings.push('Addon base URL is not a valid URL.');
    }
    if (!form.apiToken.trim()) warnings.push('DebridNest API token is required.');
    if (!form.jackettApiKey.trim()) {
      warnings.push('Jackett key is blank; this only works when the addon can read the bundled Jackett config.');
    }
    const prewarmCount = Number(form.prewarmCount);
    if (prewarmCount > 0 && form.prewarmMode === 'all') {
      warnings.push('Prewarm can start downloads while browsing Stremio results.');
    }
    return warnings;
  }, [form]);

  async function testManifest(e: FormEvent) {
    e.preventDefault();
    setBusy('manifest');
    setManifestResult(null);
    try {
      const res = await fetch(manifestUrl, { cache: 'no-store' });
      if (!res.ok) throw new Error(`Manifest HTTP ${res.status}`);
      const manifest = await res.json();
      if (!manifest?.id || !manifest?.resources) {
        throw new Error('Manifest response is missing required Stremio fields');
      }
      setManifestResult({ id: manifest.id, name: manifest.name });
      toast('Manifest loaded');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Manifest test failed', 'error');
    } finally {
      setBusy(null);
    }
  }

  async function runDiagnostics() {
    setBusy('diagnostics');
    setDiagnostics(null);
    try {
      const res = await fetch(diagnosticsUrl, { cache: 'no-store' });
      const body = await res.json().catch(() => null);
      if (!res.ok) {
        throw new Error(body?.error || `Diagnostics HTTP ${res.status}`);
      }
      setDiagnostics(body);
      toast(body?.ok ? 'Addon checks passed' : 'Addon checks need attention', body?.ok ? 'success' : 'error');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Diagnostics failed', 'error');
    } finally {
      setBusy(null);
    }
  }

  if (loading && !config) {
    return <p className="muted page-loading">Loading Stremio setup…</p>;
  }

  return (
    <form className="stremio-setup" onSubmit={testManifest}>
      <section className="card stremio-hero-card">
        <div className="card-heading">
          <h2>Install</h2>
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            onClick={() => refresh()}
          >
            <Icon name="rotate-cw" size={14} />
            Refresh
          </button>
        </div>
        <div className="stremio-install-actions">
          <a className="btn btn-primary" href={installUrl || undefined}>
            <Icon name="external-link" size={15} />
            Install in Stremio
          </a>
          <button type="submit" className="btn btn-secondary" disabled={!manifestUrl || busy === 'manifest'}>
            <Icon name="check" size={15} />
            {busy === 'manifest' ? 'Testing…' : 'Test manifest'}
          </button>
          <button
            type="button"
            className="btn btn-secondary"
            disabled={!diagnosticsUrl || busy === 'diagnostics'}
            onClick={runDiagnostics}
          >
            <Icon name="activity" size={15} />
            {busy === 'diagnostics' ? 'Checking…' : 'Run checks'}
          </button>
        </div>
        <div className="url-list">
          <UrlBox label="Manifest URL" url={manifestUrl} />
          <UrlBox label="Stremio install URL" url={installUrl} />
        </div>
      </section>

      <section className="card">
        <div className="card-heading">
          <h2>Connection</h2>
        </div>
        <div className="form-row">
          <div className="form-group">
            <label htmlFor="stremio-addon-base">Addon base URL</label>
            <input
              id="stremio-addon-base"
              className="input input-mono"
              type="url"
              value={form.addonBaseUrl}
              placeholder="http://127.0.0.1:7001"
              onChange={(e) => {
                setDirtyBase(true);
                setForm((current) => ({ ...current, addonBaseUrl: e.target.value }));
              }}
            />
          </div>
          <div className="form-group">
            <label htmlFor="stremio-label">Addon label</label>
            <input
              id="stremio-label"
              className="input"
              value={form.manifestLabel}
              placeholder="Living room"
              onChange={(e) => setForm((current) => ({ ...current, manifestLabel: e.target.value }))}
            />
          </div>
        </div>
        <div className="form-row">
          <div className="form-group">
            <label htmlFor="stremio-api-url">DebridNest API URL</label>
            <input
              id="stremio-api-url"
              className="input input-mono"
              value={form.apiUrl}
              onChange={(e) => setForm((current) => ({ ...current, apiUrl: e.target.value }))}
            />
          </div>
          <div className="form-group">
            <label htmlFor="stremio-api-token">DebridNest API token</label>
            <input
              id="stremio-api-token"
              className="input input-mono"
              type="password"
              value={form.apiToken}
              autoComplete="off"
              onChange={(e) => setForm((current) => ({ ...current, apiToken: e.target.value }))}
            />
          </div>
        </div>
        <div className="form-row">
          <div className="form-group">
            <label htmlFor="stremio-jackett-url">Jackett/Prowlarr URL</label>
            <input
              id="stremio-jackett-url"
              className="input input-mono"
              value={form.jackettUrl}
              onChange={(e) => setForm((current) => ({ ...current, jackettUrl: e.target.value }))}
            />
          </div>
          <div className="form-group">
            <label htmlFor="stremio-jackett-key">Jackett/Prowlarr API key</label>
            <input
              id="stremio-jackett-key"
              className="input input-mono"
              type="password"
              value={form.jackettApiKey}
              autoComplete="off"
              onChange={(e) => setForm((current) => ({ ...current, jackettApiKey: e.target.value }))}
            />
          </div>
        </div>
      </section>

      <section className="card">
        <div className="card-heading">
          <h2>Stream rules</h2>
        </div>
        <div className="form-row">
          <div className="form-group">
            <label htmlFor="stremio-preset">Preset</label>
            <select
              id="stremio-preset"
              className="input"
              value={form.preset}
              onChange={(e) => setForm((current) => ({ ...current, preset: e.target.value }))}
            >
              <option>Default</option>
              <option>1080p SDR</option>
              <option>4K SDR</option>
              <option>4K HDR</option>
              <option>Small files</option>
            </select>
          </div>
          <div className="form-group">
            <label htmlFor="stremio-max-results">Max results</label>
            <input
              id="stremio-max-results"
              className="input"
              type="number"
              min={1}
              max={50}
              value={form.maxResults}
              onChange={(e) => setForm((current) => ({ ...current, maxResults: e.target.value }))}
            />
          </div>
          <div className="form-group">
            <label htmlFor="stremio-resolution">Max resolution</label>
            <select
              id="stremio-resolution"
              className="input"
              value={form.maxResolution}
              onChange={(e) => setForm((current) => ({ ...current, maxResolution: e.target.value }))}
            >
              <option>Any</option>
              <option>720</option>
              <option>1080</option>
              <option>2160</option>
            </select>
          </div>
          <div className="form-group">
            <label htmlFor="stremio-max-size">Max file size (GB)</label>
            <input
              id="stremio-max-size"
              className="input"
              type="number"
              min={0}
              value={form.maxFileSizeGb}
              onChange={(e) => setForm((current) => ({ ...current, maxFileSizeGb: e.target.value }))}
            />
          </div>
        </div>
        <div className="form-row">
          <div className="form-group">
            <label htmlFor="stremio-quality-filter">Quality allowlist</label>
            <input
              id="stremio-quality-filter"
              className="input"
              value={form.qualityFilter}
              placeholder="1080p,720p"
              onChange={(e) => setForm((current) => ({ ...current, qualityFilter: e.target.value }))}
            />
          </div>
          <div className="form-group">
            <label htmlFor="stremio-source-filter">Source allowlist</label>
            <input
              id="stremio-source-filter"
              className="input"
              value={form.sourceFilter}
              placeholder="BluRay,WEB-DL,WEBRip"
              onChange={(e) => setForm((current) => ({ ...current, sourceFilter: e.target.value }))}
            />
          </div>
        </div>
        <div className="toggle-grid">
          <Toggle
            checked={form.preferSdr}
            onChange={(checked) => setForm((current) => ({ ...current, preferSdr: checked }))}
            label="Prefer SDR"
          />
          <Toggle
            checked={form.showStreamHealth}
            onChange={(checked) => setForm((current) => ({ ...current, showStreamHealth: checked }))}
            label="Show stream health"
          />
        </div>
      </section>

      <section className="card">
        <div className="card-heading">
          <h2>Prewarm</h2>
          <span className={Number(form.prewarmCount) > 0 ? 'pill pill-warning' : 'pill pill-muted'}>
            {Number(form.prewarmCount) > 0 ? 'Enabled' : 'Off'}
          </span>
        </div>
        <div className="form-row">
          <div className="form-group">
            <label htmlFor="stremio-prewarm-count">Top streams</label>
            <input
              id="stremio-prewarm-count"
              className="input"
              type="number"
              min={0}
              max={10}
              value={form.prewarmCount}
              onChange={(e) => setForm((current) => ({ ...current, prewarmCount: e.target.value }))}
            />
          </div>
          <div className="form-group">
            <label htmlFor="stremio-prewarm-mode">Strategy</label>
            <select
              id="stremio-prewarm-mode"
              className="input"
              value={form.prewarmMode}
              onChange={(e) => setForm((current) => ({ ...current, prewarmMode: e.target.value }))}
            >
              <option value="off">Off</option>
              <option value="cached">Cached only</option>
              <option value="all">All eligible</option>
            </select>
          </div>
          <div className="form-group">
            <label htmlFor="stremio-prewarm-indexers">Indexer allowlist</label>
            <input
              id="stremio-prewarm-indexers"
              className="input"
              value={form.prewarmIndexers}
              placeholder="limetorrents,magnetz"
              onChange={(e) => setForm((current) => ({ ...current, prewarmIndexers: e.target.value }))}
            />
          </div>
        </div>
      </section>

      <section className="card">
        <div className="card-heading">
          <h2>Checks</h2>
          {error && <span className="pill pill-warning">{error}</span>}
        </div>
        <div className="stremio-check-list">
          <CheckRow
            label="Manifest"
            ok={manifestResult ? true : undefined}
            detail={manifestResult ? `${manifestResult.name || 'Addon'} (${manifestResult.id})` : 'Run a manifest test'}
          />
          <CheckRow
            label="DebridNest API"
            ok={diagnostics?.debridnest?.ok}
            detail={
              diagnostics?.debridnest?.ok
                ? diagnostics.debridnest.username || 'Authenticated'
                : diagnostics?.debridnest?.error || 'Run addon checks'
            }
          />
          <CheckRow
            label="Jackett/Prowlarr"
            ok={diagnostics?.jackett?.ok}
            detail={
              diagnostics?.jackett?.ok
                ? `${diagnostics.jackett.configuredIndexers ?? 0} indexers configured`
                : diagnostics?.jackett?.error || 'Run addon checks'
            }
          />
          <CheckRow
            label="Prewarm"
            ok={diagnostics?.prewarm ? true : undefined}
            detail={
              diagnostics?.prewarm
                ? `${diagnostics.prewarm.mode}, top ${diagnostics.prewarm.count}`
                : `${form.prewarmMode}, top ${form.prewarmCount}`
            }
          />
        </div>
        {[...localWarnings, ...(diagnostics?.hints ?? [])].length > 0 && (
          <ul className="stremio-warning-list">
            {[...localWarnings, ...(diagnostics?.hints ?? [])].map((warning) => (
              <li key={warning}>{warning}</li>
            ))}
          </ul>
        )}
      </section>
    </form>
  );
}
