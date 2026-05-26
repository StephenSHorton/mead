<script lang="ts">
  import { onDestroy } from 'svelte'
  import type { main } from '../../wailsjs/go/models'
  import { InstallApp, LaunchApp, ListProcesses } from '../../wailsjs/go/main/App.js'
  import ProcessList from './ProcessList.svelte'
  import { fmtDate, errMessage } from './format'

  export let bottle: main.BottleSummary

  let processes: main.ProcessSummary[] = []
  let procsLoading = true
  let procsError = ''

  let showInstall = false
  let installerPath = ''
  let installing = false
  let installError = ''
  let lastInstallRunID = ''

  let showLaunch = false
  let exePath = ''
  let launching = false
  let launchError = ''
  let lastLaunchRunID = ''

  const POLL_MS = 2000
  let pollTimer: ReturnType<typeof setTimeout> | null = null
  let cancelled = false

  async function refresh() {
    try {
      const list = await ListProcesses(bottle.id)
      if (cancelled) return
      processes = list || []
      procsError = ''
    } catch (err) {
      if (cancelled) return
      procsError = errMessage(err)
    } finally {
      if (!cancelled) procsLoading = false
    }
  }

  async function loop() {
    if (cancelled) return
    await refresh()
    if (cancelled) return
    pollTimer = setTimeout(loop, POLL_MS)
  }

  // Kick off polling immediately on mount.
  loop()

  onDestroy(() => {
    cancelled = true
    if (pollTimer) {
      clearTimeout(pollTimer)
      pollTimer = null
    }
  })

  function openInstall() {
    showInstall = true
    showLaunch = false
    installerPath = ''
    installError = ''
    lastInstallRunID = ''
  }

  function cancelInstall() {
    showInstall = false
    installerPath = ''
    installError = ''
  }

  async function submitInstall() {
    const p = installerPath.trim()
    if (!p || installing) return
    installing = true
    installError = ''
    try {
      const runID = await InstallApp(bottle.id, p, [])
      lastInstallRunID = runID
      installerPath = ''
      // Pull in the newly-started process immediately rather than waiting for the next poll tick.
      await refresh()
    } catch (err) {
      installError = errMessage(err)
    } finally {
      installing = false
    }
  }

  function openLaunch() {
    showLaunch = true
    showInstall = false
    exePath = ''
    launchError = ''
    lastLaunchRunID = ''
  }

  function cancelLaunch() {
    showLaunch = false
    exePath = ''
    launchError = ''
  }

  async function submitLaunch() {
    const p = exePath.trim()
    if (!p || launching) return
    launching = true
    launchError = ''
    try {
      const runID = await LaunchApp(bottle.id, p)
      lastLaunchRunID = runID
      exePath = ''
      await refresh()
    } catch (err) {
      launchError = errMessage(err)
    } finally {
      launching = false
    }
  }

  function focus(el: HTMLInputElement) {
    el.focus()
  }
</script>

<div class="detail">
  <dl class="meta">
    <div>
      <dt>ID</dt>
      <dd><code>{bottle.id}</code></dd>
    </div>
    {#if bottle.wine_version}
      <div>
        <dt>Wine</dt>
        <dd><code>{bottle.wine_version}</code></dd>
      </div>
    {/if}
    {#if bottle.created_at}
      <div>
        <dt>Created</dt>
        <dd>{fmtDate(bottle.created_at)}</dd>
      </div>
    {/if}
  </dl>

  <div class="actions">
    <button class="btn-ghost" on:click={openInstall} disabled={showInstall}>Run installer…</button>
    <button class="btn-ghost" on:click={openLaunch} disabled={showLaunch}>Launch app…</button>
  </div>

  {#if showInstall}
    <form class="run-form" on:submit|preventDefault={submitInstall}>
      <label for="installer-path">Installer path on this Mac (e.g. <code>/Users/you/Downloads/setup.exe</code>)</label>
      <div class="run-form-row">
        <input
          id="installer-path"
          type="text"
          placeholder="/path/to/installer.exe"
          bind:value={installerPath}
          disabled={installing}
          use:focus
        />
        <button type="submit" class="btn-primary" disabled={installing || !installerPath.trim()}>
          {installing ? 'Running…' : 'Install'}
        </button>
        <button type="button" class="btn-ghost" on:click={cancelInstall} disabled={installing}>
          Cancel
        </button>
      </div>
      {#if installError}<p class="error">{installError}</p>{/if}
      {#if lastInstallRunID}
        <p class="hint">Started run <code>{lastInstallRunID}</code></p>
      {/if}
    </form>
  {/if}

  {#if showLaunch}
    <form class="run-form" on:submit|preventDefault={submitLaunch}>
      <label for="exe-path">Executable path (relative to <code>drive_c</code> or absolute)</label>
      <div class="run-form-row">
        <input
          id="exe-path"
          type="text"
          placeholder="Program Files/MyApp/app.exe"
          bind:value={exePath}
          disabled={launching}
          use:focus
        />
        <button type="submit" class="btn-primary" disabled={launching || !exePath.trim()}>
          {launching ? 'Launching…' : 'Launch'}
        </button>
        <button type="button" class="btn-ghost" on:click={cancelLaunch} disabled={launching}>
          Cancel
        </button>
      </div>
      {#if launchError}<p class="error">{launchError}</p>{/if}
      {#if lastLaunchRunID}
        <p class="hint">Started run <code>{lastLaunchRunID}</code></p>
      {/if}
    </form>
  {/if}

  <ProcessList {processes} loading={procsLoading} error={procsError} />
</div>

<style>
  .detail {
    padding: 0.5rem 0.25rem 0.25rem;
  }

  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 1rem 1.5rem;
    margin: 0 0 0.75rem;
  }

  .meta div {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .meta dt {
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: #9aa3b1;
    font-weight: 600;
    margin: 0;
  }

  .meta dd {
    margin: 0;
    font-size: 0.85rem;
    color: #e6e6e6;
  }

  code {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    background: rgba(0, 0, 0, 0.3);
    padding: 0.1rem 0.35rem;
    border-radius: 4px;
    font-size: 0.8rem;
  }

  .actions {
    display: flex;
    gap: 0.5rem;
    margin-bottom: 0.5rem;
  }

  .btn-ghost {
    font-family: inherit;
    font-size: 0.8rem;
    padding: 0.3rem 0.65rem;
    border-radius: 5px;
    background: transparent;
    color: #9aa3b1;
    border: 1px solid rgba(255, 255, 255, 0.12);
    cursor: pointer;
    transition: color 120ms ease, border-color 120ms ease;
  }

  .btn-ghost:hover:not(:disabled) {
    color: #e6e6e6;
    border-color: rgba(255, 255, 255, 0.24);
  }

  .btn-ghost:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-primary {
    font-family: inherit;
    font-size: 0.8rem;
    padding: 0.3rem 0.7rem;
    border-radius: 5px;
    background: #3b82f6;
    color: white;
    border: 1px solid #3b82f6;
    cursor: pointer;
  }

  .btn-primary:hover:not(:disabled) {
    background: #2563eb;
    border-color: #2563eb;
  }

  .btn-primary:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .run-form {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    margin-bottom: 0.6rem;
    padding: 0.65rem 0.75rem;
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 6px;
  }

  .run-form label {
    font-size: 0.78rem;
    color: #9aa3b1;
  }

  .run-form-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }

  .run-form input {
    flex: 1;
    min-width: 220px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 5px;
    color: #e6e6e6;
    padding: 0.35rem 0.55rem;
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 0.82rem;
  }

  .run-form input:focus {
    outline: none;
    border-color: #3b82f6;
  }

  .hint {
    color: #6b7480;
    font-size: 0.78rem;
    margin: 0;
  }

  .error {
    color: #f87171;
    font-size: 0.82rem;
    margin: 0;
  }
</style>
