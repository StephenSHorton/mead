<script lang="ts">
  import { onMount } from 'svelte'
  import { BridgePort, BridgeTokenShort } from '../wailsjs/go/main/App.js'

  let port: number | null = null
  let tokenShort: string = ''

  onMount(async () => {
    port = await BridgePort()
    tokenShort = await BridgeTokenShort()
  })
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

  <section class="status">
    <p>Pre-alpha skeleton. Bottle management lands next.</p>
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
    margin: 0 0 0.5rem;
    font-size: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: #9aa3b1;
    font-weight: 600;
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

  .status p {
    margin: 0;
    color: #9aa3b1;
  }
</style>
