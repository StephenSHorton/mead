<script lang="ts">
  import { tick } from 'svelte'
  import { ProcessLogs, KillProcess } from '../../wailsjs/go/main/App.js'
  import { errMessage } from './format'

  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Alert, AlertDescription } from '$lib/components/ui/alert'
  import SquareIcon from '@lucide/svelte/icons/square'
  import AlertCircleIcon from '@lucide/svelte/icons/alert-circle'
  import ArrowDownIcon from '@lucide/svelte/icons/arrow-down'

  let {
    runID,
    initialExited = false,
    exitCode = undefined as number | undefined,
  }: {
    runID: string
    initialExited?: boolean
    exitCode?: number
  } = $props()

  let bytes = $state('')
  let offset = $state(0)
  let exited = $state(false)
  let loadError = $state('')

  // Seed `exited` from the initial prop once at mount.
  $effect(() => {
    exited = initialExited
  })

  let killing = $state(false)
  let killError = $state('')

  let scrollContainer = $state<HTMLDivElement | null>(null)
  let atBottom = $state(true)

  const CHUNK = 65536
  const INTERVAL_MS = 750

  function isNearBottom(el: HTMLElement | null): boolean {
    if (!el) return true
    return el.scrollHeight - el.scrollTop - el.clientHeight < 40
  }

  function onScroll() {
    atBottom = isNearBottom(scrollContainer)
  }

  async function scrollToBottom() {
    await tick()
    if (scrollContainer) {
      scrollContainer.scrollTop = scrollContainer.scrollHeight
      atBottom = true
    }
  }

  // Incremental polling — fetch (offset → chunk), append, schedule next.
  // After the server reports exited=true we do one more pass to drain any
  // bytes written between the prior poll and exit, then stop. Errors back off.
  $effect(() => {
    let cancelled = false
    let drained = false
    let timer: ReturnType<typeof setTimeout> | null = null

    // Reset state when runID changes (parent uses {#key} so this shouldn't
    // re-run for an existing instance, but keep the reset for safety).
    bytes = ''
    offset = 0
    loadError = ''
    atBottom = true

    async function pollOnce() {
      if (cancelled) return
      try {
        const chunk = await ProcessLogs(runID, offset, CHUNK)
        if (cancelled) return
        if (chunk.bytes && chunk.bytes.length > 0) {
          const wasNearBottom = isNearBottom(scrollContainer)
          bytes += chunk.bytes
          if (wasNearBottom) void scrollToBottom()
        }
        offset = chunk.next_offset
        if (exited && !drained) {
          drained = true
          return
        }
        if (chunk.exited) {
          exited = true
          // Schedule a final drain pass.
          timer = setTimeout(pollOnce, 200)
          return
        }
        loadError = ''
        timer = setTimeout(pollOnce, INTERVAL_MS)
      } catch (err) {
        loadError = errMessage(err)
        if (!cancelled && !exited) {
          timer = setTimeout(pollOnce, 1500)
        }
      }
    }

    void pollOnce()

    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  })

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
</script>

<div class="bg-card mt-2 rounded-md border p-2">
  <div class="mb-2 flex items-center gap-2">
    <span class="text-muted-foreground font-mono text-[0.7rem]">
      run {runID.slice(0, 8)}
    </span>
    {#if exited}
      <Badge variant={exitCode === 0 ? 'secondary' : 'destructive'} class="text-[0.65rem]">
        Exited{exitCode !== undefined ? ` · code ${exitCode}` : ''}
      </Badge>
    {:else}
      <Badge class="text-[0.65rem]">Running</Badge>
      <Button
        variant="destructive"
        size="xs"
        class="ml-auto"
        onclick={kill}
        disabled={killing}
      >
        <SquareIcon />
        {killing ? 'Killing…' : 'Kill'}
      </Button>
    {/if}
  </div>

  {#if killError}
    <Alert variant="destructive" class="mb-2">
      <AlertCircleIcon />
      <AlertDescription>kill: {killError}</AlertDescription>
    </Alert>
  {/if}
  {#if loadError}
    <Alert variant="destructive" class="mb-2">
      <AlertCircleIcon />
      <AlertDescription>logs: {loadError}</AlertDescription>
    </Alert>
  {/if}

  <!--
    Log surface. We kept the plain scrollable <div> rather than shadcn's
    <ScrollArea> — ScrollArea uses a virtual viewport which fights the
    "stick to bottom while content streams" pattern (the viewport's scroll
    position resets relative to a wrapped child, so isNearBottom() returns
    stale values during paint). The plain div with bg-card / border / rounded
    matches shadcn surface styling.
  -->
  <div class="relative">
    <div
      bind:this={scrollContainer}
      onscroll={onScroll}
      class="bg-background text-foreground/90 h-[360px] overflow-auto overscroll-none rounded-md border"
    >
      <pre
        class="m-0 p-2 font-mono text-[0.72rem] leading-relaxed whitespace-pre-wrap break-all"
      >{bytes || (exited ? '(no output)' : 'Waiting for output…')}</pre>
    </div>
    {#if !atBottom}
      <Button
        size="xs"
        variant="outline"
        class="absolute right-2 bottom-2 shadow-sm"
        onclick={scrollToBottom}
      >
        <ArrowDownIcon />
        Scroll to bottom
      </Button>
    {/if}
  </div>
</div>
