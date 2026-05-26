<script lang="ts">
  import { onDestroy } from 'svelte'
  import { ProcessLogs, KillProcess } from '../../wailsjs/go/main/App.js'
  import { errMessage } from './format'

  export let runID: string
  // Initial exited state from the parent; the polling response is the source of truth after that.
  export let initialExited: boolean = false
  export let exitCode: number | undefined = undefined

  let bytes = ''
  let offset = 0
  let exited = initialExited
  let drained = false
  let loadError = ''

  let killing = false
  let killError = ''

  let timer: ReturnType<typeof setTimeout> | null = null
  let cancelled = false
  let logEl: HTMLPreElement | null = null

  const CHUNK = 65536
  const INTERVAL_MS = 750

  async function pollOnce() {
    if (cancelled) return
    try {
      const chunk = await ProcessLogs(runID, offset, CHUNK)
      if (cancelled) return
      if (chunk.bytes && chunk.bytes.length > 0) {
        const wasNearBottom = logEl
          ? logEl.scrollHeight - logEl.scrollTop - logEl.clientHeight < 40
          : true
        bytes += chunk.bytes
        if (logEl && wasNearBottom) {
          // queue scroll after Svelte updates the DOM
          setTimeout(() => {
            if (logEl) logEl.scrollTop = logEl.scrollHeight
          }, 0)
        }
      }
      offset = chunk.next_offset
      // If we previously saw exited but were polling once more to drain the tail, stop now.
      if (exited && !drained) {
        drained = true
        return
      }
      if (chunk.exited) {
        exited = true
        // Schedule one more pass to drain anything written between the prior poll and exit.
        timer = setTimeout(pollOnce, 200)
        return
      }
      loadError = ''
      timer = setTimeout(pollOnce, INTERVAL_MS)
    } catch (err) {
      loadError = errMessage(err)
      if (!cancelled && !exited) {
        // Back off briefly on error, then retry.
        timer = setTimeout(pollOnce, 1500)
      }
    }
  }

  async function kill() {
    if (killing || exited) return
    killing = true
    killError = ''
    try {
      await KillProcess(runID)
    } catch (err) {
      killError = errMessage(err)
    } finally {
      killing = false
    }
  }

  // Kick off the polling loop. runID is set at mount and not expected to change for a given viewer.
  pollOnce()

  onDestroy(() => {
    cancelled = true
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
  })
</script>

<div class="logs">
  <div class="logs-head">
    <span class="run-id" title={runID}>run {runID.slice(0, 8)}</span>
    {#if exited}
      <span class="status exited">
        Exited{exitCode !== undefined ? ` · code ${exitCode}` : ''}
      </span>
    {:else}
      <span class="status running">Running</span>
      <button class="btn-danger" on:click={kill} disabled={killing}>
        {killing ? 'Killing…' : 'Kill'}
      </button>
    {/if}
  </div>

  {#if killError}
    <p class="error">kill: {killError}</p>
  {/if}
  {#if loadError}
    <p class="error">logs: {loadError}</p>
  {/if}

  <pre class="log-body" bind:this={logEl}>{bytes || (exited ? '(no output)' : 'Waiting for output…')}</pre>
</div>

<style>
  .logs {
    margin-top: 0.5rem;
    background: rgba(0, 0, 0, 0.35);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 6px;
    padding: 0.6rem 0.75rem;
  }

  .logs-head {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    margin-bottom: 0.4rem;
  }

  .run-id {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 0.75rem;
    color: #9aa3b1;
  }

  .status {
    font-size: 0.75rem;
    padding: 0.1rem 0.4rem;
    border-radius: 3px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-weight: 600;
  }

  .status.running {
    background: rgba(59, 130, 246, 0.15);
    color: #93c5fd;
  }

  .status.exited {
    background: rgba(255, 255, 255, 0.06);
    color: #9aa3b1;
  }

  .btn-danger {
    font-family: inherit;
    font-size: 0.75rem;
    padding: 0.2rem 0.55rem;
    border-radius: 4px;
    background: #b91c1c;
    color: white;
    border: 1px solid #b91c1c;
    cursor: pointer;
    margin-left: auto;
  }

  .btn-danger:hover:not(:disabled) {
    background: #991b1b;
    border-color: #991b1b;
  }

  .btn-danger:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .log-body {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 0.78rem;
    line-height: 1.4;
    color: #d1d5db;
    background: rgba(0, 0, 0, 0.45);
    border-radius: 4px;
    padding: 0.5rem 0.6rem;
    margin: 0;
    max-height: 280px;
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-all;
  }

  .error {
    color: #f87171;
    font-size: 0.8rem;
    margin: 0 0 0.4rem;
  }
</style>
