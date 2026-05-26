<script lang="ts">
  import { onMount } from 'svelte'
  import {
    BridgePort,
    BridgeTokenShort,
    ListBottles,
    CreateBottle,
    DeleteBottle,
  } from '../wailsjs/go/main/App.js'
  import type { main } from '../wailsjs/go/models'
  import BottleDetail from './lib/BottleDetail.svelte'

  let port: number | null = null
  let tokenShort: string = ''

  let bottles: main.BottleSummary[] = []
  let bottlesLoaded = false

  let showCreate = false
  let newName = ''
  let creating = false
  let createError = ''

  let confirmId: string | null = null
  let deletingId: string | null = null
  let deleteError = ''

  let expandedId: string | null = null

  function toggleExpand(id: string) {
    // Collapse if the user clicks the row that's already open; otherwise switch focus.
    expandedId = expandedId === id ? null : id
    // Cancelling delete-confirm when toggling avoids a stale confirm prompt across modes.
    if (confirmId && confirmId !== id) confirmId = null
  }

  function onRowKey(e: KeyboardEvent, id: string) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      toggleExpand(id)
    }
  }

  onMount(async () => {
    port = await BridgePort()
    tokenShort = await BridgeTokenShort()
    await refresh()
  })

  async function refresh() {
    bottles = await ListBottles()
    bottlesLoaded = true
  }

  function openCreate() {
    showCreate = true
    newName = ''
    createError = ''
  }

  function cancelCreate() {
    showCreate = false
    newName = ''
    createError = ''
  }

  async function submitCreate() {
    const name = newName.trim()
    if (!name || creating) return
    creating = true
    createError = ''
    try {
      await CreateBottle(name)
      showCreate = false
      newName = ''
      await refresh()
    } catch (err: any) {
      createError = typeof err === 'string' ? err : err?.message ?? String(err)
    } finally {
      creating = false
    }
  }

  function askDelete(id: string) {
    confirmId = id
    deleteError = ''
  }

  function cancelDelete() {
    confirmId = null
    deleteError = ''
  }

  async function confirmDelete(id: string) {
    if (deletingId) return
    deletingId = id
    deleteError = ''
    try {
      await DeleteBottle(id)
      confirmId = null
      if (expandedId === id) expandedId = null
      await refresh()
    } catch (err: any) {
      deleteError = typeof err === 'string' ? err : err?.message ?? String(err)
    } finally {
      deletingId = null
    }
  }

  function focus(el: HTMLInputElement) {
    el.focus()
  }

  function fmtDate(iso?: string) {
    if (!iso) return ''
    const d = new Date(iso)
    if (isNaN(d.getTime())) return iso
    return d.toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' })
  }
</script>

<main>
  <header>
    <h1>Mead</h1>
    <p class="tagline">Agent-driven Wine for macOS</p>
  </header>

  <section class="bridge">
    <h2>MCP bridge</h2>
    {#if port && port > 0}
      <code>127.0.0.1:{port}</code>
      <span class="token">token: {tokenShort}…</span>
    {:else}
      <span class="muted">not running</span>
    {/if}
  </section>

  <section class="bottles">
    <div class="section-head">
      <h2>Bottles</h2>
      {#if !showCreate}
        <button class="btn-primary" on:click={openCreate}>Create bottle</button>
      {/if}
    </div>

    {#if showCreate}
      <form class="create-form" on:submit|preventDefault={submitCreate}>
        <input
          type="text"
          placeholder="bottle name"
          bind:value={newName}
          disabled={creating}
          use:focus
        />
        <button type="submit" class="btn-primary" disabled={creating || !newName.trim()}>
          {creating ? 'Creating…' : 'Create'}
        </button>
        <button type="button" class="btn-ghost" on:click={cancelCreate} disabled={creating}>
          Cancel
        </button>
        {#if creating}
          <span class="hint">running wineboot, this takes 5–15s…</span>
        {/if}
        {#if createError}
          <p class="error">{createError}</p>
        {/if}
      </form>
    {/if}

    {#if !bottlesLoaded}
      <p class="muted small">Loading…</p>
    {:else if bottles.length === 0}
      <p class="empty">No bottles yet. Create one to get started.</p>
    {:else}
      <ul class="bottle-list">
        {#each bottles as b (b.id)}
          <li
            class="bottle-row"
            class:confirming={confirmId === b.id}
            class:expanded={expandedId === b.id}
          >
            <div
              class="bottle-main"
              role="button"
              tabindex="0"
              aria-expanded={expandedId === b.id}
              on:click={() => toggleExpand(b.id)}
              on:keydown={(e) => onRowKey(e, b.id)}
            >
              <span class="caret" aria-hidden="true">{expandedId === b.id ? '▾' : '▸'}</span>
              <div class="bottle-text">
                <span class="name">{b.name}</span>
                <span class="meta">
                  {#if b.wine_version}<span class="wine">{b.wine_version}</span>{/if}
                  {#if b.created_at}<span class="date">{fmtDate(b.created_at)}</span>{/if}
                </span>
              </div>
            </div>
            <div class="bottle-actions">
              {#if confirmId === b.id}
                <span class="confirm-text">Delete this bottle?</span>
                <button
                  class="btn-danger"
                  on:click|stopPropagation={() => confirmDelete(b.id)}
                  disabled={deletingId === b.id}
                >
                  {deletingId === b.id ? 'Deleting…' : 'Delete'}
                </button>
                <button
                  class="btn-ghost"
                  on:click|stopPropagation={cancelDelete}
                  disabled={deletingId === b.id}
                >
                  Cancel
                </button>
              {:else}
                <button
                  class="btn-delete"
                  on:click|stopPropagation={() => askDelete(b.id)}
                  title="Delete bottle"
                >
                  Delete
                </button>
              {/if}
            </div>
            {#if confirmId === b.id && deleteError}
              <p class="error row-error">{deleteError}</p>
            {/if}
            {#if expandedId === b.id}
              <div class="bottle-detail-wrap">
                {#key b.id}
                  <BottleDetail bottle={b} />
                {/key}
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>
</main>

<style>
  main {
    font-family: -apple-system, BlinkMacSystemFont, 'SF Pro Text', sans-serif;
    color: #e6e6e6;
    padding: 2rem;
    max-width: 720px;
    margin: 0 auto;
  }

  header h1 {
    margin: 0;
    font-size: 2.4rem;
    letter-spacing: -0.02em;
  }

  .tagline {
    margin: 0.25rem 0 2rem;
    color: #9aa3b1;
    font-size: 0.95rem;
  }

  section {
    background: rgba(255, 255, 255, 0.04);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 8px;
    padding: 1rem 1.25rem;
    margin-bottom: 1rem;
  }

  section h2 {
    margin: 0;
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: #9aa3b1;
    font-weight: 600;
  }

  .bridge h2 {
    margin-bottom: 0.5rem;
  }

  code {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
    background: rgba(0, 0, 0, 0.3);
    padding: 0.15rem 0.4rem;
    border-radius: 4px;
  }

  .token {
    margin-left: 0.75rem;
    color: #9aa3b1;
    font-size: 0.9rem;
  }

  .muted {
    color: #6b7480;
  }

  .small {
    font-size: 0.85rem;
  }

  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.75rem;
  }

  button {
    font-family: inherit;
    font-size: 0.85rem;
    padding: 0.35rem 0.75rem;
    border-radius: 5px;
    border: 1px solid transparent;
    cursor: pointer;
    transition: background 120ms ease, border-color 120ms ease, color 120ms ease;
  }

  button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .btn-primary {
    background: #3b82f6;
    color: white;
    border-color: #3b82f6;
  }

  .btn-primary:hover:not(:disabled) {
    background: #2563eb;
    border-color: #2563eb;
  }

  .btn-ghost {
    background: transparent;
    color: #9aa3b1;
    border-color: rgba(255, 255, 255, 0.12);
  }

  .btn-ghost:hover:not(:disabled) {
    color: #e6e6e6;
    border-color: rgba(255, 255, 255, 0.24);
  }

  .btn-danger {
    background: #b91c1c;
    color: white;
    border-color: #b91c1c;
  }

  .btn-danger:hover:not(:disabled) {
    background: #991b1b;
    border-color: #991b1b;
  }

  .btn-delete {
    background: transparent;
    color: #6b7480;
    border-color: transparent;
    padding: 0.25rem 0.5rem;
    font-size: 0.8rem;
    opacity: 0;
    transition: opacity 120ms ease, color 120ms ease;
  }

  .bottle-row:hover .btn-delete,
  .btn-delete:focus {
    opacity: 1;
  }

  .btn-delete:hover {
    color: #ef4444;
  }

  .create-form {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
    margin-bottom: 0.75rem;
    padding: 0.75rem;
    background: rgba(0, 0, 0, 0.2);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 6px;
  }

  .create-form input {
    flex: 1;
    min-width: 200px;
    background: rgba(0, 0, 0, 0.3);
    border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 5px;
    color: #e6e6e6;
    padding: 0.4rem 0.6rem;
    font-family: inherit;
    font-size: 0.9rem;
  }

  .create-form input:focus {
    outline: none;
    border-color: #3b82f6;
  }

  .hint {
    color: #6b7480;
    font-size: 0.8rem;
  }

  .error {
    color: #f87171;
    font-size: 0.85rem;
    margin: 0;
    flex-basis: 100%;
  }

  .row-error {
    margin-top: 0.5rem;
  }

  .empty {
    color: #6b7480;
    margin: 0.5rem 0 0;
    font-size: 0.9rem;
  }

  .bottle-list {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .bottle-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    padding: 0.6rem 0.25rem;
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  }

  .bottle-row:last-child {
    border-bottom: none;
  }

  .bottle-row.confirming {
    background: rgba(185, 28, 28, 0.08);
    border-radius: 5px;
    padding-left: 0.5rem;
    padding-right: 0.5rem;
  }

  .bottle-main {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    min-width: 0;
    flex: 1;
    background: transparent;
    border: none;
    padding: 0.1rem 0.2rem;
    margin: -0.1rem -0.2rem;
    border-radius: 4px;
    cursor: pointer;
    color: inherit;
    text-align: left;
    transition: background 120ms ease;
  }

  .bottle-main:hover {
    background: rgba(255, 255, 255, 0.04);
  }

  .bottle-main:focus-visible {
    outline: none;
    background: rgba(255, 255, 255, 0.06);
    box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.4);
  }

  .bottle-text {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    min-width: 0;
  }

  .caret {
    color: #6b7480;
    font-size: 0.75rem;
    width: 0.85rem;
    text-align: center;
  }

  .bottle-row.expanded {
    background: rgba(255, 255, 255, 0.025);
    border-radius: 6px;
    padding-left: 0.5rem;
    padding-right: 0.5rem;
  }

  .bottle-row.expanded .caret {
    color: #9aa3b1;
  }

  .bottle-detail-wrap {
    flex-basis: 100%;
    border-top: 1px solid rgba(255, 255, 255, 0.05);
    margin-top: 0.5rem;
    padding-top: 0.5rem;
  }

  .name {
    font-size: 0.95rem;
    color: #e6e6e6;
  }

  .meta {
    display: flex;
    gap: 0.75rem;
    color: #6b7480;
    font-size: 0.8rem;
  }

  .wine {
    font-family: ui-monospace, 'SF Mono', Menlo, monospace;
  }

  .bottle-actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .confirm-text {
    color: #f87171;
    font-size: 0.85rem;
  }
</style>
