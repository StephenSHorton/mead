<script lang="ts">
  import type { main } from '../../wailsjs/go/models'
  import LogViewer from './LogViewer.svelte'
  import { fmtRelative } from './format'

  export let processes: main.ProcessSummary[] = []
  export let loading: boolean = false
  export let error: string = ''

  let openRunID: string | null = null

  function toggleLogs(runID: string) {
    openRunID = openRunID === runID ? null : runID
  }

  function joinArgv(argv: string[] | undefined): string {
    if (!argv || argv.length === 0) return '(unknown command)'
    return argv.join(' ')
  }
</script>

<div class="proc">
  <h3>Processes</h3>

  {#if error}
    <p class="error">{error}</p>
  {/if}

  {#if loading && processes.length === 0}
    <p class="muted small">Loading…</p>
  {:else if processes.length === 0}
    <p class="empty">No processes for this bottle yet. Install or launch an app to start one.</p>
  {:else}
    <ul class="proc-list">
      {#each processes as p (p.run_id)}
        <li class="proc-row" class:open={openRunID === p.run_id}>
          <div class="proc-main">
            <code class="argv" title={joinArgv(p.argv)}>{joinArgv(p.argv)}</code>
            <span class="proc-meta">
              <span class="started">{fmtRelative(p.started_at)}</span>
              {#if p.exited}
                <span class="state exited">
                  Exited{p.exit_code !== undefined ? ` · code ${p.exit_code}` : ''}
                </span>
              {:else}
                <span class="state running">Running</span>
              {/if}
              {#if p.run_err}
                <span class="run-err" title={p.run_err}>error</span>
              {/if}
            </span>
          </div>
          <div class="proc-actions">
            <button class="btn-ghost" on:click={() => toggleLogs(p.run_id)}>
              {openRunID === p.run_id ? 'Hide logs' : 'View logs'}
            </button>
          </div>
          {#if openRunID === p.run_id}
            <div class="logs-wrap">
              {#key p.run_id}
                <LogViewer runID={p.run_id} initialExited={p.exited} exitCode={p.exit_code} />
              {/key}
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .proc {
    margin-top: 0.75rem;
  }

  h3 {
    margin: 0 0 0.5rem;
    font-size: 0.7rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: #9aa3b1;
    font-weight: 600;
  }

  .empty {
    color: #6b7480;
    margin: 0;
    font-size: 0.85rem;
  }

  .muted {
    color: #6b7480;
  }

  .small {
    font-size: 0.85rem;
  }

  .error {
    color: #f87171;
    font-size: 0.85rem;
    margin: 0 0 0.4rem;
  }

  .proc-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .proc-row {
    padding: 0.45rem 0.25rem;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.5rem;
  }

  .proc-row:last-child {
    border-bottom: none;
  }

  .proc-row.open {
    background: rgba(255, 255, 255, 0.02);
    border-radius: 5px;
    padding-left: 0.5rem;
    padding-right: 0.5rem;
  }

  .proc-main {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    min-width: 0;
    flex: 1;
  }

  .argv {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    font-size: 0.8rem;
    color: #e6e6e6;
    background: rgba(0, 0, 0, 0.3);
    padding: 0.15rem 0.4rem;
    border-radius: 4px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100%;
  }

  .proc-meta {
    display: flex;
    align-items: center;
    gap: 0.65rem;
    font-size: 0.78rem;
    color: #6b7480;
  }

  .started {
    color: #9aa3b1;
  }

  .state {
    font-size: 0.7rem;
    padding: 0.08rem 0.4rem;
    border-radius: 3px;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-weight: 600;
  }

  .state.running {
    background: rgba(59, 130, 246, 0.15);
    color: #93c5fd;
  }

  .state.exited {
    background: rgba(255, 255, 255, 0.06);
    color: #9aa3b1;
  }

  .run-err {
    color: #f87171;
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .proc-actions {
    display: flex;
    align-items: center;
    gap: 0.4rem;
  }

  .btn-ghost {
    font-family: inherit;
    font-size: 0.78rem;
    padding: 0.25rem 0.6rem;
    border-radius: 4px;
    background: transparent;
    color: #9aa3b1;
    border: 1px solid rgba(255, 255, 255, 0.12);
    cursor: pointer;
    transition: color 120ms ease, border-color 120ms ease;
  }

  .btn-ghost:hover {
    color: #e6e6e6;
    border-color: rgba(255, 255, 255, 0.24);
  }

  .logs-wrap {
    flex-basis: 100%;
  }
</style>
