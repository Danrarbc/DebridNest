import { type FormEvent, type SetStateAction, useCallback, useEffect, useState } from 'react';
import {
  createRSSImport,
  deleteRSSImport,
  fetchConfig,
  fetchRSSImports,
  fetchSettings,
  fetchStorageObjects,
  fetchSystem,
  joinUrl,
  patchSettings,
  patchTorrentFileStorage,
  purgeTorrents,
  reconcileStorage,
  runCleanup,
  runRSSImport,
  testS3Settings,
  type Config,
  type RetentionResult,
  type RSSImport,
  type Settings,
  type SettingsPatch,
  type StorageObjectEntry,
  type StorageObjectsResponse,
  type StorageReconcileResponse,
  type StorageRecordEntry,
  type SystemInfo,
} from '../api';
import CopyButton from '../components/CopyButton';
import Icon from '../components/Icon';
import Toggle from '../components/Toggle';
import { useToast } from '../components/Toast';
import { usePolling } from '../hooks/usePolling';
import { formatBytes, formatUptime } from '../lib/format';

function ConfigRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="config-row">
      <div className="config-row-text">
        <span className="config-label">{label}</span>
        <span className="config-value">{value}</span>
      </div>
    </div>
  );
}

function UrlRow({ label, url }: { label: string; url: string }) {
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

function serviceUrls(publicUrl: string) {
  const base = publicUrl.replace(/\/+$/, '');
  return {
    public: base,
    webdav: joinUrl(base, '/webdav/'),
    metrics: joinUrl(base, '/metrics'),
    qbit: joinUrl(base, '/api/v2'),
    stremio: 'http://127.0.0.1:7001/manifest.json',
    dashboard: joinUrl(base, '/dashboard/'),
  };
}

function settingsToForm(settings: Settings) {
  return {
    retentionDays: String(settings.retentionDays ?? 0),
    diskQuotaGb: String(settings.diskQuotaGb ?? 0),
    downloadRateLimitMbps: String(settings.downloadRateLimitMbps ?? 0),
    webhookDiscordUrl: settings.webhookDiscordUrl ?? '',
    webhookNtfyTopic: settings.webhookNtfyTopic ?? '',
    webhookGotifyUrl: settings.webhookGotifyUrl ?? '',
    webhookGotifyToken: settings.webhookGotifyToken ?? '',
    notifyOnDownloadComplete: settings.notifyOnDownloadComplete ?? false,
    notifyOnTorrentProblem: settings.notifyOnTorrentProblem ?? false,
    notifyOnQuotaWarning: settings.notifyOnQuotaWarning ?? false,
    s3Enabled: settings.s3Enabled ?? false,
    s3QuotaGb: String(settings.s3QuotaGb ?? 0),
    s3Endpoint: settings.s3Endpoint ?? '',
    s3Bucket: settings.s3Bucket ?? '',
    s3Region: settings.s3Region ?? 'auto',
    s3Prefix: settings.s3Prefix ?? '',
    s3AccessKey: settings.s3AccessKey ?? '',
    s3SecretKey: settings.s3SecretKey ?? '',
    s3ForcePathStyle: settings.s3ForcePathStyle ?? false,
    s3OffloadLocal: settings.s3OffloadLocal ?? false,
  };
}

type SettingsForm = ReturnType<typeof settingsToForm>;

function defaultSettingsForm(): SettingsForm {
  return {
    retentionDays: '0',
    diskQuotaGb: '0',
    downloadRateLimitMbps: '0',
    webhookDiscordUrl: '',
    webhookNtfyTopic: '',
    webhookGotifyUrl: '',
    webhookGotifyToken: '',
    notifyOnDownloadComplete: false,
    notifyOnTorrentProblem: false,
    notifyOnQuotaWarning: false,
    s3Enabled: false,
    s3QuotaGb: '0',
    s3Endpoint: '',
    s3Bucket: '',
    s3Region: 'auto',
    s3Prefix: '',
    s3AccessKey: '',
    s3SecretKey: '',
    s3ForcePathStyle: false,
    s3OffloadLocal: false,
  };
}

function formatStorageDate(value?: string): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString();
}

function storagePolicyLabel(data: StorageObjectsResponse | StorageReconcileResponse | null): string {
  const policy = data?.policy;
  if (!policy?.enabled) return 'Disabled';
  const quota = policy.quotaBytes > 0 ? formatBytes(policy.quotaBytes) : 'Unlimited';
  return `${formatBytes(policy.usedBytes)} used / ${quota}`;
}

function RecordFindingList({
  title,
  records,
}: {
  title: string;
  records: StorageRecordEntry[];
}) {
  if (records.length === 0) return null;
  return (
    <div className="storage-finding-group">
      <h3>{title}</h3>
      <ul className="storage-finding-list">
        {records.slice(0, 5).map((item) => (
          <li key={`${item.torrentId}-${item.fileId}-${item.objectKey}`}>
            <code>{item.objectKey || item.path}</code>
            <span className="muted">{item.error || item.torrentName || item.torrentId}</span>
          </li>
        ))}
      </ul>
      {records.length > 5 && <p className="muted hint-text">+{records.length - 5} more</p>}
    </div>
  );
}

function ObjectFindingList({
  title,
  objects,
}: {
  title: string;
  objects: StorageObjectEntry[];
}) {
  if (objects.length === 0) return null;
  return (
    <div className="storage-finding-group">
      <h3>{title}</h3>
      <ul className="storage-finding-list">
        {objects.slice(0, 5).map((item) => (
          <li key={item.key}>
            <code>{item.key}</code>
            <span className="muted">{formatBytes(item.bytes)}</span>
          </li>
        ))}
      </ul>
      {objects.length > 5 && <p className="muted hint-text">+{objects.length - 5} more</p>}
    </div>
  );
}

function StorageAdminPanel() {
  const { toast } = useToast();
  const [prefix, setPrefix] = useState('');
  const [objects, setObjects] = useState<StorageObjectsResponse | null>(null);
  const [reconcile, setReconcile] = useState<StorageReconcileResponse | null>(null);
  const [loadingObjects, setLoadingObjects] = useState(false);
  const [reconcileBusy, setReconcileBusy] = useState(false);
  const [rowBusy, setRowBusy] = useState<string | null>(null);

  const loadObjects = useCallback(
    async (nextPrefix: string) => {
      setLoadingObjects(true);
      try {
        const result = await fetchStorageObjects(nextPrefix);
        setObjects(result);
        setPrefix(result.prefix ?? nextPrefix);
      } catch (err) {
        toast(err instanceof Error ? err.message : 'Object browser failed', 'error');
      } finally {
        setLoadingObjects(false);
      }
    },
    [toast],
  );

  useEffect(() => {
    void loadObjects('');
  }, [loadObjects]);

  async function handleReconcile() {
    setReconcileBusy(true);
    try {
      const result = await reconcileStorage(prefix);
      setReconcile(result);
      setObjects((current) => (current ? { ...current, policy: result.policy } : current));
      toast('Storage reconciliation complete');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Reconciliation failed', 'error');
    } finally {
      setReconcileBusy(false);
    }
  }

  async function updateFilePolicy(entry: StorageObjectEntry, patch: { keepLocal?: boolean; offloadOnly?: boolean }) {
    if (!entry.torrentId || entry.fileId == null) return;
    const busyKey = `${entry.torrentId}:${entry.fileId}`;
    setRowBusy(busyKey);
    try {
      await patchTorrentFileStorage(entry.torrentId, entry.fileId, patch);
      await loadObjects(prefix);
      toast(patch.offloadOnly ? 'File set to offload-only' : 'Storage policy updated');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Policy update failed', 'error');
    } finally {
      setRowBusy(null);
    }
  }

  const policy = objects?.policy ?? reconcile?.policy;
  const entries = objects?.entries ?? [];
  const findingCount = reconcile
    ? reconcile.missingRemote.length +
      reconcile.sizeMismatches.length +
      reconcile.orphanedRemote.length +
      reconcile.unmarkedRemote.length
    : 0;

  return (
    <section className="card storage-panel">
      <div className="card-heading">
        <h2>Storage browser</h2>
        <div className="storage-actions">
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            disabled={loadingObjects}
            onClick={() => loadObjects(prefix)}
          >
            <Icon name="rotate-cw" size={14} />
            {loadingObjects ? 'Loading…' : 'Refresh'}
          </button>
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            disabled={reconcileBusy}
            onClick={handleReconcile}
          >
            <Icon name="search" size={14} />
            {reconcileBusy ? 'Checking…' : 'Reconcile'}
          </button>
        </div>
      </div>

      <div className="config-grid storage-policy-grid">
        <ConfigRow label="Object storage" value={objects?.enabled ? 'Enabled' : 'Disabled'} />
        <ConfigRow label="Quota policy" value={storagePolicyLabel(objects ?? reconcile)} />
        <ConfigRow
          label="Reserved"
          value={policy ? `${formatBytes(policy.reservedBytes)} reserved` : '—'}
        />
        <ConfigRow
          label="Offload behavior"
          value={policy?.offloadLocal ? 'Delete local by default' : 'Keep local by default'}
        />
        <ConfigRow
          label="Early offload"
          value={policy?.earlyOffload ? 'Enabled' : 'Disabled'}
        />
        <ConfigRow
          label="Objects tracked"
          value={policy ? `${policy.objectCount} object${policy.objectCount === 1 ? '' : 's'}` : '—'}
        />
      </div>

      <div className="storage-prefix-row">
        <div className="form-group">
          <label htmlFor="storage-prefix">Prefix</label>
          <input
            id="storage-prefix"
            className="input input-mono"
            value={prefix}
            placeholder="bucket prefix"
            onChange={(e) => setPrefix(e.target.value)}
          />
        </div>
        <button
          type="button"
          className="btn btn-secondary"
          disabled={loadingObjects}
          onClick={() => loadObjects(prefix)}
        >
          Browse
        </button>
      </div>

      {!objects?.enabled && (
        <p className="muted hint-text">Enable object storage and save settings to browse a bucket.</p>
      )}

      {objects?.enabled && entries.length === 0 && !loadingObjects && (
        <p className="muted hint-text">No objects found for this prefix.</p>
      )}

      {entries.length > 0 && (
        <div className="table-wrap storage-table-wrap">
          <table>
            <thead>
              <tr>
                <th>Object</th>
                <th>Size</th>
                <th>Modified</th>
                <th>Status</th>
                <th className="actions-cell">Policy</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((entry) => {
                const busyKey =
                  entry.torrentId && entry.fileId != null
                    ? `${entry.torrentId}:${entry.fileId}`
                    : '';
                const linked = Boolean(entry.torrentId && entry.fileId != null);
                return (
                  <tr key={entry.key}>
                    <td className="name-cell">
                      {entry.directory ? (
                        <button
                          type="button"
                          className="storage-prefix-button"
                          onClick={() => loadObjects(entry.key)}
                        >
                          <Icon name="chevron-right" size={14} />
                          {entry.name || entry.key}
                        </button>
                      ) : (
                        <>
                          <span className="name-primary">{entry.name || entry.key}</span>
                          <span className="name-meta">{entry.key}</span>
                          {entry.torrentName && (
                            <span className="name-meta">{entry.torrentName}</span>
                          )}
                        </>
                      )}
                    </td>
                    <td className="num-cell">{entry.directory ? '—' : formatBytes(entry.bytes)}</td>
                    <td>{entry.directory ? '—' : formatStorageDate(entry.lastModified)}</td>
                    <td>
                      {entry.directory ? (
                        <span className="pill pill-muted">folder</span>
                      ) : linked ? (
                        <span className="storage-pills">
                          <span className="pill pill-live">tracked</span>
                          <span className={entry.localStored ? 'pill pill-accent' : 'pill pill-muted'}>
                            {entry.localStored ? 'local' : 'remote-only'}
                          </span>
                        </span>
                      ) : (
                        <span className="pill pill-warning">untracked</span>
                      )}
                    </td>
                    <td className="actions-cell">
                      {!entry.directory && linked && (
                        <div className="storage-row-actions">
                          <label className="storage-check">
                            <input
                              type="checkbox"
                              checked={Boolean(entry.keepLocal)}
                              disabled={rowBusy === busyKey}
                              onChange={(e) =>
                                updateFilePolicy(entry, { keepLocal: e.currentTarget.checked })
                              }
                            />
                            Keep local
                          </label>
                          <button
                            type="button"
                            className="btn btn-secondary btn-sm"
                            disabled={rowBusy === busyKey || !entry.localStored}
                            onClick={() => updateFilePolicy(entry, { offloadOnly: true })}
                          >
                            Offload only
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {objects?.truncated && (
        <p className="muted hint-text">Object list is truncated. Narrow the prefix to inspect more.</p>
      )}

      {reconcile && (
        <div className="storage-reconcile">
          <div className="storage-reconcile-summary">
            <span className={findingCount > 0 ? 'pill pill-warning' : 'pill pill-live'}>
              {findingCount} finding{findingCount === 1 ? '' : 's'}
            </span>
            <span className="muted">
              {reconcile.matchedCount} matched · {reconcile.dbRemoteCount} DB remote ·{' '}
              {reconcile.objectCount} bucket objects
            </span>
          </div>
          <div className="storage-finding-grid">
            <RecordFindingList title="Missing remote" records={reconcile.missingRemote} />
            <RecordFindingList title="Size mismatch" records={reconcile.sizeMismatches} />
            <ObjectFindingList title="Orphaned remote" objects={reconcile.orphanedRemote} />
            <RecordFindingList title="Unmarked remote" records={reconcile.unmarkedRemote} />
          </div>
        </div>
      )}
    </section>
  );
}

function RSSImportsPanel() {
  const { toast } = useToast();
  const [items, setItems] = useState<RSSImport[]>([]);
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [loading, setLoading] = useState(false);
  const [busyId, setBusyId] = useState<string | null>(null);

  const loadImports = useCallback(async () => {
    setLoading(true);
    try {
      setItems(await fetchRSSImports());
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Failed to load RSS imports', 'error');
    } finally {
      setLoading(false);
    }
  }, [toast]);

  useEffect(() => {
    void loadImports();
  }, [loadImports]);

  async function handleCreate(e: FormEvent) {
    e.preventDefault();
    const trimmedName = name.trim();
    const trimmedUrl = url.trim();
    if (!trimmedName || !trimmedUrl) {
      toast('Name and URL are required', 'error');
      return;
    }

    setLoading(true);
    try {
      await createRSSImport({ name: trimmedName, url: trimmedUrl });
      setName('');
      setUrl('');
      await loadImports();
      toast('RSS import added');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Failed to add RSS import', 'error');
    } finally {
      setLoading(false);
    }
  }

  async function handleRun(item: RSSImport) {
    setBusyId(item.id);
    try {
      const result = await runRSSImport(item.id);
      await loadImports();
      toast(`RSS import checked ${result.found}, queued ${result.added}`);
      if (result.errors.length > 0) {
        toast(`${result.errors.length} item${result.errors.length === 1 ? '' : 's'} failed`, 'error');
      }
    } catch (err) {
      toast(err instanceof Error ? err.message : 'RSS import failed', 'error');
    } finally {
      setBusyId(null);
    }
  }

  async function handleDelete(item: RSSImport) {
    if (!confirm(`Delete RSS import "${item.name}"?`)) return;
    setBusyId(item.id);
    try {
      await deleteRSSImport(item.id);
      setItems((current) => current.filter((entry) => entry.id !== item.id));
      toast('RSS import deleted');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Delete failed', 'error');
    } finally {
      setBusyId(null);
    }
  }

  return (
    <section className="card imports-panel">
      <div className="card-heading">
        <h2>RSS imports</h2>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          disabled={loading}
          onClick={() => loadImports()}
        >
          <Icon name="rotate-cw" size={14} />
          Refresh
        </button>
      </div>
      <p className="section-desc">
        Add RSS or Torznab feeds that expose magnet links, then run them manually to queue new
        torrents.
      </p>
      <form className="rss-import-form" onSubmit={handleCreate}>
        <div className="form-group">
          <label htmlFor="rss-import-name">Name</label>
          <input
            id="rss-import-name"
            className="input"
            value={name}
            placeholder="Movie watchlist"
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div className="form-group">
          <label htmlFor="rss-import-url">Feed URL</label>
          <input
            id="rss-import-url"
            className="input input-mono"
            type="url"
            value={url}
            placeholder="https://indexer.example/rss"
            onChange={(e) => setUrl(e.target.value)}
          />
        </div>
        <button type="submit" className="btn btn-primary" disabled={loading}>
          Add feed
        </button>
      </form>

      {items.length === 0 ? (
        <p className="muted hint-text">
          {loading ? 'Loading imports…' : 'No RSS imports configured yet.'}
        </p>
      ) : (
        <div className="table-wrap imports-table-wrap">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Last run</th>
                <th>Result</th>
                <th className="actions-cell">Actions</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id}>
                  <td className="name-cell">
                    <span className="name-primary">{item.name}</span>
                    <span className="name-meta">{item.url}</span>
                  </td>
                  <td>{item.lastRunAt ? new Date(item.lastRunAt).toLocaleString() : 'Never'}</td>
                  <td>
                    <span className={item.lastError ? 'pill pill-warning' : 'pill pill-muted'}>
                      {item.lastResult || 'Not run'}
                    </span>
                    {item.lastRunAt && (
                      <span className="name-meta">
                        {item.lastAdded} queued{item.lastError ? ` · ${item.lastError}` : ''}
                      </span>
                    )}
                  </td>
                  <td className="actions-cell">
                    <span className="row-actions">
                      <button
                        type="button"
                        className="btn btn-secondary btn-sm"
                        disabled={busyId === item.id}
                        onClick={() => handleRun(item)}
                      >
                        Run now
                      </button>
                      <button
                        type="button"
                        className="icon-btn icon-btn-danger"
                        title="Delete RSS import"
                        aria-label={`Delete ${item.name}`}
                        disabled={busyId === item.id}
                        onClick={() => handleDelete(item)}
                      >
                        <Icon name="trash-2" size={18} />
                      </button>
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

interface SettingsProps {
  isAdmin: boolean;
}

export default function SettingsPage({ isAdmin }: SettingsProps) {
  const { toast } = useToast();
  const [cleanupResult, setCleanupResult] = useState<RetentionResult | null>(null);
  const [cleanupError, setCleanupError] = useState<string | null>(null);
  const [cleanupBusy, setCleanupBusy] = useState(false);
  const [purgeBusy, setPurgeBusy] = useState<'completed' | 'failed' | null>(null);
  const [purgeMessage, setPurgeMessage] = useState<string | null>(null);
  const [saveBusy, setSaveBusy] = useState(false);
  const [s3TestBusy, setS3TestBusy] = useState(false);
  const [formDirty, setFormDirty] = useState(false);

  const [form, setFormState] = useState<SettingsForm>(() => defaultSettingsForm());
  const setForm = useCallback((updater: SetStateAction<SettingsForm>) => {
    setFormDirty(true);
    setFormState(updater);
  }, []);

  const configLoader = useCallback(async () => {
    const [config, system, settings] = await Promise.all([
      fetchConfig(),
      fetchSystem(),
      fetchSettings(),
    ]);
    return { config, system, settings };
  }, []);

  const { data, error, loading, refresh } = usePolling(configLoader, { intervalMs: 30000 });

  useEffect(() => {
    if (data?.settings && !formDirty) {
      setFormState(settingsToForm(data.settings));
    }
  }, [data?.settings, formDirty]);

  async function handleSave(e: FormEvent) {
    e.preventDefault();
    setSaveBusy(true);

    const patch: SettingsPatch = {
      retentionDays: Number(form.retentionDays) || 0,
      diskQuotaGb: Number(form.diskQuotaGb) || 0,
      downloadRateLimitMbps: Number(form.downloadRateLimitMbps) || 0,
      webhookDiscordUrl: form.webhookDiscordUrl.trim(),
      webhookNtfyTopic: form.webhookNtfyTopic.trim(),
      webhookGotifyUrl: form.webhookGotifyUrl.trim(),
      webhookGotifyToken: form.webhookGotifyToken.trim(),
      notifyOnDownloadComplete: form.notifyOnDownloadComplete,
      notifyOnTorrentProblem: form.notifyOnTorrentProblem,
      notifyOnQuotaWarning: form.notifyOnQuotaWarning,
    };

    if (isAdmin) {
      patch.s3Enabled = form.s3Enabled;
      patch.s3QuotaGb = Number(form.s3QuotaGb) || 0;
      patch.s3Endpoint = form.s3Endpoint.trim();
      patch.s3Bucket = form.s3Bucket.trim();
      patch.s3Region = form.s3Region.trim() || 'auto';
      patch.s3Prefix = form.s3Prefix.trim();
      patch.s3ForcePathStyle = form.s3ForcePathStyle;
      patch.s3OffloadLocal = form.s3OffloadLocal;
      if (form.s3AccessKey.trim() && form.s3AccessKey !== '(configured)') {
        patch.s3AccessKey = form.s3AccessKey.trim();
      }
      if (form.s3SecretKey.trim() && form.s3SecretKey !== '(configured)') {
        patch.s3SecretKey = form.s3SecretKey.trim();
      }
    }

    try {
      const updated = await patchSettings(patch);
      setFormState(settingsToForm(updated));
      setFormDirty(false);
      toast('Settings saved');
      await refresh();
    } catch (err) {
      toast(err instanceof Error ? err.message : 'Save failed', 'error');
    } finally {
      setSaveBusy(false);
    }
  }

  async function handleS3Test() {
    setS3TestBusy(true);
    try {
      await testS3Settings();
      toast('S3 connection successful');
    } catch (err) {
      toast(err instanceof Error ? err.message : 'S3 test failed', 'error');
    } finally {
      setS3TestBusy(false);
    }
  }

  async function handleCleanup() {
    setCleanupBusy(true);
    setCleanupError(null);
    setCleanupResult(null);
    try {
      const result = await runCleanup();
      setCleanupResult(result);
      await refresh();
    } catch (err) {
      setCleanupError(err instanceof Error ? err.message : 'Cleanup failed');
    } finally {
      setCleanupBusy(false);
    }
  }

  async function handlePurge(filter: 'completed' | 'failed') {
    const label = filter === 'completed' ? 'all completed' : 'all failed';
    if (!confirm(`Delete ${label} torrents and their files? This cannot be undone.`)) return;

    setPurgeBusy(filter);
    setPurgeMessage(null);
    try {
      const result = await purgeTorrents(filter);
      setPurgeMessage(`Removed ${result.deleted} torrent${result.deleted === 1 ? '' : 's'}.`);
      await refresh();
    } catch (err) {
      setPurgeMessage(err instanceof Error ? err.message : 'Purge failed');
    } finally {
      setPurgeBusy(null);
    }
  }

  if (loading && !data) {
    return <p className="muted page-loading">Loading settings…</p>;
  }

  if (error && !data) {
    return (
      <div className="page-error card">
        <p className="error">{error}</p>
        <button type="button" className="btn btn-secondary" onClick={() => refresh()}>
          Retry
        </button>
      </div>
    );
  }

  if (!data) return null;

  const { config, system } = data as {
    config: Config;
    system: SystemInfo;
    settings: Settings;
  };
  const urls = serviceUrls(config.publicUrl);

  return (
    <div className="settings">
      <form className="settings-editable" onSubmit={handleSave}>
        <section className="card">
          <div className="card-heading">
            <h2>Limits &amp; retention</h2>
            {isAdmin && (
              <button type="submit" className="btn btn-primary btn-sm" disabled={saveBusy}>
                {saveBusy ? 'Saving…' : 'Save changes'}
              </button>
            )}
          </div>
          <p className="section-desc">
            {isAdmin
              ? 'Adjust retention, disk quota, and download rate limit. Use 0 to disable a limit.'
              : 'Current limits (read-only). Contact an admin to change these values.'}
          </p>
          <div className="form-row">
            <div className="form-group">
              <label htmlFor="retention-days">Retention (days)</label>
              <input
                id="retention-days"
                className="input"
                type="number"
                min={0}
                value={form.retentionDays}
                disabled={!isAdmin}
                onChange={(e) => setForm((f) => ({ ...f, retentionDays: e.target.value }))}
              />
            </div>
            <div className="form-group">
              <label htmlFor="disk-quota">Disk quota (GB)</label>
              <input
                id="disk-quota"
                className="input"
                type="number"
                min={0}
                value={form.diskQuotaGb}
                disabled={!isAdmin}
                onChange={(e) => setForm((f) => ({ ...f, diskQuotaGb: e.target.value }))}
              />
            </div>
            <div className="form-group">
              <label htmlFor="rate-limit">Download cap (MB/s)</label>
              <input
                id="rate-limit"
                className="input"
                type="number"
                min={0}
                value={form.downloadRateLimitMbps}
                disabled={!isAdmin}
                onChange={(e) =>
                  setForm((f) => ({ ...f, downloadRateLimitMbps: e.target.value }))
                }
              />
              <p className="form-hint">0 = unlimited</p>
            </div>
          </div>
        </section>

        <section className="card">
          <div className="card-heading">
            <h2>Notifications</h2>
          </div>
          <p className="section-desc">
            Webhook targets and event toggles for download notifications.
          </p>
          <div className="form-row">
            <div className="form-group">
              <label htmlFor="discord-webhook">Discord webhook URL</label>
              <input
                id="discord-webhook"
                className="input input-mono"
                type="url"
                value={form.webhookDiscordUrl}
                disabled={!isAdmin}
                placeholder="https://discord.com/api/webhooks/…"
                onChange={(e) => setForm((f) => ({ ...f, webhookDiscordUrl: e.target.value }))}
              />
            </div>
            <div className="form-group">
              <label htmlFor="ntfy-topic">ntfy topic</label>
              <input
                id="ntfy-topic"
                className="input input-mono"
                value={form.webhookNtfyTopic}
                disabled={!isAdmin}
                placeholder="debridnest-alerts"
                onChange={(e) => setForm((f) => ({ ...f, webhookNtfyTopic: e.target.value }))}
              />
            </div>
          </div>
          <div className="form-row">
            <div className="form-group">
              <label htmlFor="gotify-url">Gotify URL</label>
              <input
                id="gotify-url"
                className="input input-mono"
                type="url"
                value={form.webhookGotifyUrl}
                disabled={!isAdmin}
                placeholder="https://gotify.example.com"
                onChange={(e) => setForm((f) => ({ ...f, webhookGotifyUrl: e.target.value }))}
              />
            </div>
            <div className="form-group">
              <label htmlFor="gotify-token">Gotify token</label>
              <input
                id="gotify-token"
                className="input input-mono"
                type="password"
                value={form.webhookGotifyToken}
                disabled={!isAdmin}
                autoComplete="off"
                onChange={(e) => setForm((f) => ({ ...f, webhookGotifyToken: e.target.value }))}
              />
            </div>
          </div>
          <div className="toggle-grid">
            <Toggle
              checked={form.notifyOnDownloadComplete}
              disabled={!isAdmin}
              onChange={(checked) =>
                setForm((f) => ({ ...f, notifyOnDownloadComplete: checked }))
              }
              label="Notify on download complete"
            />
            <Toggle
              checked={form.notifyOnTorrentProblem}
              disabled={!isAdmin}
              onChange={(checked) => setForm((f) => ({ ...f, notifyOnTorrentProblem: checked }))}
              label="Notify on failed or stuck torrents"
            />
            <Toggle
              checked={form.notifyOnQuotaWarning}
              disabled={!isAdmin}
              onChange={(checked) => setForm((f) => ({ ...f, notifyOnQuotaWarning: checked }))}
              label={<span>Notify when disk quota &gt;90%</span>}
            />
          </div>
        </section>

        {isAdmin && (
          <section className="card">
            <div className="card-heading">
              <h2>Object storage (S3)</h2>
              <Toggle
                checked={form.s3Enabled}
                onChange={(checked) => setForm((f) => ({ ...f, s3Enabled: checked }))}
                label={form.s3Enabled ? 'Enabled' : 'Disabled'}
              />
            </div>
            <p className="section-desc">
              Upload completed files to S3-compatible storage (AWS S3, Cloudflare R2, Backblaze B2).
              Save settings before testing the connection.
            </p>
            <div
              style={{
                opacity: form.s3Enabled ? 1 : 0.45,
                pointerEvents: form.s3Enabled ? 'auto' : 'none',
              }}
            >
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="s3-endpoint">Endpoint</label>
                  <input
                    id="s3-endpoint"
                    className="input input-mono"
                    type="url"
                    value={form.s3Endpoint}
                    placeholder="https://accountid.r2.cloudflarestorage.com"
                    onChange={(e) => setForm((f) => ({ ...f, s3Endpoint: e.target.value }))}
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="s3-bucket">Bucket</label>
                  <input
                    id="s3-bucket"
                    className="input input-mono"
                    value={form.s3Bucket}
                    placeholder="debridnest"
                    onChange={(e) => setForm((f) => ({ ...f, s3Bucket: e.target.value }))}
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="s3-quota">S3 quota (GB)</label>
                  <input
                    id="s3-quota"
                    className="input"
                    type="number"
                    min={0}
                    value={form.s3QuotaGb}
                    onChange={(e) => setForm((f) => ({ ...f, s3QuotaGb: e.target.value }))}
                  />
                  <p className="form-hint">0 = unlimited</p>
                </div>
              </div>
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="s3-region">Region</label>
                  <input
                    id="s3-region"
                    className="input input-mono"
                    value={form.s3Region}
                    placeholder="auto"
                    onChange={(e) => setForm((f) => ({ ...f, s3Region: e.target.value }))}
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="s3-prefix">Key prefix</label>
                  <input
                    id="s3-prefix"
                    className="input input-mono"
                    value={form.s3Prefix}
                    placeholder="debridnest/"
                    onChange={(e) => setForm((f) => ({ ...f, s3Prefix: e.target.value }))}
                  />
                </div>
              </div>
              <div className="form-row">
                <div className="form-group">
                  <label htmlFor="s3-access-key">Access key</label>
                  <input
                    id="s3-access-key"
                    className="input input-mono"
                    type="password"
                    value={form.s3AccessKey}
                    placeholder="(configured)"
                    autoComplete="off"
                    onChange={(e) => setForm((f) => ({ ...f, s3AccessKey: e.target.value }))}
                  />
                </div>
                <div className="form-group">
                  <label htmlFor="s3-secret-key">Secret key</label>
                  <input
                    id="s3-secret-key"
                    className="input input-mono"
                    type="password"
                    value={form.s3SecretKey}
                    placeholder="(configured)"
                    autoComplete="off"
                    onChange={(e) => setForm((f) => ({ ...f, s3SecretKey: e.target.value }))}
                  />
                </div>
              </div>
              <div className="toggle-grid" style={{ marginTop: 0, marginBottom: 14 }}>
                <Toggle
                  checked={form.s3ForcePathStyle}
                  onChange={(checked) => setForm((f) => ({ ...f, s3ForcePathStyle: checked }))}
                  label="Force path-style URLs"
                />
                <Toggle
                  checked={form.s3OffloadLocal}
                  onChange={(checked) => setForm((f) => ({ ...f, s3OffloadLocal: checked }))}
                  label="Delete local file after upload"
                />
              </div>
            </div>
            {form.s3Enabled && (
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                disabled={s3TestBusy}
                onClick={handleS3Test}
              >
                {s3TestBusy ? 'Testing…' : 'Test connection'}
              </button>
            )}
          </section>
        )}

        {isAdmin && <StorageAdminPanel />}

        {isAdmin && <RSSImportsPanel />}
      </form>

      <section className="card">
        <div className="card-heading">
          <h2>Service URLs</h2>
        </div>
        <div className="url-list">
          <UrlRow label="Public URL" url={urls.public} />
          <UrlRow label="WebDAV" url={urls.webdav} />
          <UrlRow label="Metrics" url={urls.metrics} />
          <UrlRow label="qBittorrent API (Sonarr/Radarr)" url={urls.qbit} />
          <UrlRow label="Stremio addon" url={urls.stremio} />
          <UrlRow label="Dashboard" url={urls.dashboard} />
        </div>
        <p className="hint-text">
          Stremio addon runs separately (default port 7001). Use the Stremio dashboard page to
          generate, test, and install a personalized manifest URL.
        </p>
      </section>

      <section className="card">
        <div className="card-heading">
          <h2>Configuration</h2>
        </div>
        <div className="config-grid">
          <ConfigRow label="Public URL" value={config.publicUrl || '—'} />
          <ConfigRow label="WebDAV" value={config.webdavEnabled ? 'Enabled' : 'Disabled'} />
          <ConfigRow label="Metrics" value={config.metricsEnabled ? 'Enabled' : 'Disabled'} />
          {config.s3Enabled != null && (
            <ConfigRow label="Object storage" value={config.s3Enabled ? 'Enabled' : 'Disabled'} />
          )}
          {config.s3QuotaGb != null && (
            <ConfigRow
              label="S3 quota"
              value={config.s3QuotaGb > 0 ? `${config.s3QuotaGb} GB` : 'Not set'}
            />
          )}
          {config.linkTtlHours != null && (
            <ConfigRow label="Link TTL" value={`${config.linkTtlHours} hours`} />
          )}
          {config.autoSelectSeconds != null && (
            <ConfigRow label="Auto-select" value={`${config.autoSelectSeconds}s`} />
          )}
          {config.qbitUser && <ConfigRow label="qBit user" value={config.qbitUser} />}
          {config.seedAfterComplete != null && (
            <ConfigRow label="Seeding" value={config.seedAfterComplete ? 'Enabled' : 'Disabled'} />
          )}
          {config.transcodeEnabled != null && (
            <ConfigRow label="Transcode" value={config.transcodeEnabled ? 'Enabled' : 'Disabled'} />
          )}
        </div>
      </section>

      <section className="card">
        <div className="card-heading">
          <h2>System</h2>
        </div>
        <div className="config-grid">
          <ConfigRow label="Version" value={system.version || '—'} />
          <ConfigRow label="Listen" value={system.listen || '—'} />
          <ConfigRow label="Torrent port" value={String(system.torrentPort ?? '—')} />
          <ConfigRow label="Uptime" value={formatUptime(system.uptimeSeconds)} />
          <ConfigRow
            label="Started"
            value={system.startedAt ? new Date(system.startedAt).toLocaleString() : '—'}
          />
        </div>
        {system.features && Object.keys(system.features).length > 0 && (
          <div className="feature-tags">
            {Object.entries(system.features).map(([key, enabled]) => (
              <span key={key} className={enabled ? 'pill pill-live' : 'pill pill-muted'}>
                {key.replace(/_/g, ' ')}
              </span>
            ))}
          </div>
        )}
      </section>

      <section className="card">
        <div className="card-heading">
          <h2>Maintenance</h2>
        </div>
        <p className="section-desc">
          Run retention cleanup now, or purge finished torrents. Purges cannot be undone.
        </p>
        <div className="maintenance-actions">
          <button
            type="button"
            className="btn btn-secondary"
            disabled={cleanupBusy}
            onClick={handleCleanup}
          >
            <Icon name="rotate-cw" />
            {cleanupBusy ? 'Running cleanup…' : 'Run cleanup'}
          </button>
          <button
            type="button"
            className="btn btn-danger"
            disabled={purgeBusy !== null}
            onClick={() => handlePurge('completed')}
          >
            {purgeBusy === 'completed' ? 'Deleting…' : 'Purge completed'}
          </button>
          <button
            type="button"
            className="btn btn-danger"
            disabled={purgeBusy !== null}
            onClick={() => handlePurge('failed')}
          >
            {purgeBusy === 'failed' ? 'Deleting…' : 'Purge failed'}
          </button>
        </div>
        {cleanupResult && (
          <p className="success-msg">
            Cleanup complete — {cleanupResult.ageRemoved} removed by age,{' '}
            {cleanupResult.quotaRemoved} by disk quota, {cleanupResult.s3QuotaRemoved ?? 0} by
            S3 quota. Disk: {formatBytes(cleanupResult.diskUsed)}
            {cleanupResult.diskQuota > 0 && ` / ${formatBytes(cleanupResult.diskQuota)}`}. S3:{' '}
            {formatBytes(cleanupResult.s3Used ?? 0)}
            {(cleanupResult.s3Quota ?? 0) > 0 && ` / ${formatBytes(cleanupResult.s3Quota ?? 0)}`}.
          </p>
        )}
        {cleanupError && <p className="error hint-text">{cleanupError}</p>}
        {purgeMessage && <p className="muted hint-text">{purgeMessage}</p>}
      </section>

      {error && <p className="error">{error}</p>}
    </div>
  );
}
