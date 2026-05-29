<script lang="ts">
  import {
    BridgePort,
    BridgeTokenShort,
    ListBottles,
    DeleteBottle,
    CloneBottle,
  } from '../wailsjs/go/main/App.js'
  import { EventsOn } from '../wailsjs/runtime/runtime'
  import type { main } from '../wailsjs/go/models'
  import BottleDetail from './lib/BottleDetail.svelte'
  import EmptyBottleState from './lib/components/EmptyBottleState.svelte'
  import AppSidebar from './lib/components/AppSidebar.svelte'
  import CommandPalette from './lib/components/CommandPalette.svelte'
  import { errMessage } from './lib/format'
  import { ui } from './lib/store.svelte'

  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Sidebar from '$lib/components/ui/sidebar'
  import { Toaster } from '$lib/components/ui/sonner'
  import { toast } from 'svelte-sonner'
  import CommandIcon from '@lucide/svelte/icons/command'
  import SearchIcon from '@lucide/svelte/icons/search'

  let port = $state<number | null>(null)
  let tokenShort = $state('')

  let bottles = $state<main.BottleSummary[]>([])
  let bottlesLoaded = $state(false)

  let confirmId = $state<string | null>(null)
  let deletingId = $state<string | null>(null)

  // Clone dialog state. cloneSource non-null = dialog open.
  let cloneSource = $state<main.BottleSummary | null>(null)
  let cloneName = $state('')
  let cloning = $state(false)

  // The selected bottle is computed from the shared store + current list.
  let selected = $derived(
    bottles.find((b) => b.id === ui.selectedBottleId) || null
  )

  // Mount + lifecycle: connect to bridge, prime bottle list, subscribe to
  // mead:bottles-changed. The $effect cleanup unsubscribes on unmount.
  $effect(() => {
    let alive = true
    let unsub: (() => void) | null = null
    ;(async () => {
      try {
        port = await BridgePort()
        tokenShort = await BridgeTokenShort()
      } catch (err) {
        if (alive) toast.error('Bridge unreachable', { description: errMessage(err) })
      }
      unsub = EventsOn('mead:bottles-changed', () => {
        if (alive) void refresh()
      })
      await refresh()
    })()
    return () => {
      alive = false
      if (unsub) unsub()
    }
  })

  // BottleDetail's "Clone" button dispatches mead:clone-bottle; open the
  // clone dialog pre-filled with a sensible default name.
  $effect(() => {
    function onClone(e: Event) {
      const d = (e as CustomEvent<{ id: string; name: string }>).detail
      askClone(d.id)
    }
    window.addEventListener('mead:clone-bottle', onClone)
    return () => window.removeEventListener('mead:clone-bottle', onClone)
  })

  // If the selected bottle disappears (e.g. deleted out-of-band) clear the
  // selection so the empty state renders instead of a stale detail pane.
  $effect(() => {
    if (
      ui.selectedBottleId &&
      bottlesLoaded &&
      !bottles.some((b) => b.id === ui.selectedBottleId)
    ) {
      ui.selectBottle(null)
    }
  })

  async function refresh() {
    try {
      bottles = (await ListBottles()) || []
    } catch (err) {
      toast.error('Failed to list bottles', { description: errMessage(err) })
    } finally {
      bottlesLoaded = true
    }
  }

  function askDelete(id: string) {
    confirmId = id
  }

  function cancelDelete() {
    confirmId = null
  }

  async function confirmDelete() {
    if (!confirmId || deletingId) return
    const id = confirmId
    deletingId = id
    try {
      await DeleteBottle(id)
      toast.success('Bottle deleted')
      confirmId = null
      if (ui.selectedBottleId === id) ui.selectBottle(null)
      await refresh()
    } catch (err) {
      toast.error('Delete failed', { description: errMessage(err) })
    } finally {
      deletingId = null
    }
  }

  function askClone(id: string) {
    const b = bottles.find((x) => x.id === id)
    if (!b) return
    cloneSource = b
    cloneName = `${b.name} copy`
  }

  function cancelClone() {
    cloneSource = null
  }

  async function confirmClone() {
    if (!cloneSource || cloning) return
    const name = cloneName.trim()
    if (!name) return
    cloning = true
    const toastId = toast.loading('Cloning bottle…', {
      description: 'clonefile copy — usually instant',
    })
    try {
      const b = await CloneBottle(cloneSource.id, name)
      toast.success('Bottle cloned', { id: toastId, description: name })
      cloneSource = null
      if (b && b.id) ui.selectBottle(b.id)
      await refresh()
    } catch (err) {
      toast.error('Clone failed', { id: toastId, description: errMessage(err) })
    } finally {
      cloning = false
    }
  }

  function openCreateBottle() {
    // The "+ New bottle" form lives inside AppSidebar; the palette's
    // "Create bottle" action just opens the palette-side trigger.
    // Lightest path: dispatch a custom event the sidebar listens for.
    window.dispatchEvent(new CustomEvent('mead:new-bottle'))
  }
</script>

<Toaster richColors position="bottom-right" />

<Sidebar.Provider style="--sidebar-width: 17rem;">
  <AppSidebar {bottles} loading={!bottlesLoaded} onAskDelete={askDelete} />

  <Sidebar.Inset class="flex min-h-svh min-w-0 flex-col">
    <!-- Top bar across the inset (right of sidebar) -->
    <header class="bg-background sticky top-0 z-10 flex h-12 items-center gap-2 border-b px-3">
      <Sidebar.Trigger />
      <h1 class="text-base font-semibold tracking-tight">Mead</h1>

      <div class="ml-auto flex items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          class="text-muted-foreground gap-2"
          onclick={() => ui.openPalette()}
        >
          <SearchIcon />
          <span class="hidden sm:inline">Search commands…</span>
          <kbd class="bg-muted text-muted-foreground ml-1 hidden items-center gap-0.5 rounded px-1.5 py-0.5 font-mono text-[0.65rem] sm:inline-flex">
            <CommandIcon class="size-3" />K
          </kbd>
        </Button>
        {#if port && port > 0}
          <Badge variant="secondary" class="font-mono text-xs">
            <span class="text-muted-foreground">MCP</span>
            127.0.0.1:{port}
            <span class="text-muted-foreground">·</span>
            <span class="text-muted-foreground">{tokenShort}…</span>
          </Badge>
        {:else}
          <Badge variant="outline" class="text-xs">bridge offline</Badge>
        {/if}
      </div>
    </header>

    <main class="flex-1 overflow-auto p-6">
      {#if selected}
        {#key selected.id}
          <BottleDetail bottle={selected} />
        {/key}
      {:else}
        <EmptyBottleState hasBottles={bottles.length > 0} />
      {/if}
    </main>
  </Sidebar.Inset>
</Sidebar.Provider>

<CommandPalette
  {bottles}
  onNewBottle={openCreateBottle}
  onDeleteBottle={askDelete}
  onCloneBottle={askClone}
/>

<!-- Delete confirmation dialog -->
<Dialog.Root
  open={confirmId !== null}
  onOpenChange={(open) => {
    if (!open) cancelDelete()
  }}
>
  <Dialog.Content>
    <Dialog.Header>
      <Dialog.Title>Delete bottle?</Dialog.Title>
      <Dialog.Description>
        This permanently removes the bottle's wine prefix and all installed apps inside it.
        This action can't be undone.
      </Dialog.Description>
    </Dialog.Header>
    <Dialog.Footer>
      <Button variant="ghost" onclick={cancelDelete} disabled={deletingId !== null}>
        Cancel
      </Button>
      <Button
        variant="destructive"
        onclick={confirmDelete}
        disabled={deletingId !== null}
      >
        {deletingId !== null ? 'Deleting…' : 'Delete'}
      </Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<!-- Clone bottle dialog -->
<Dialog.Root
  open={cloneSource !== null}
  onOpenChange={(open) => {
    if (!open) cancelClone()
  }}
>
  <Dialog.Content>
    <Dialog.Header>
      <Dialog.Title>Clone bottle</Dialog.Title>
      <Dialog.Description>
        Copies <span class="font-medium">{cloneSource?.name}</span>'s entire wine prefix
        (apps, registry, settings) into a new bottle. On APFS this is a near-instant
        copy-on-write clone.
      </Dialog.Description>
    </Dialog.Header>
    <form
      onsubmit={(e) => {
        e.preventDefault()
        void confirmClone()
      }}
    >
      <Label for="clone-name" class="text-xs">New bottle name</Label>
      <!-- svelte-ignore a11y_autofocus -->
      <Input
        id="clone-name"
        class="mt-1.5"
        bind:value={cloneName}
        disabled={cloning}
        autofocus
      />
      <Dialog.Footer class="mt-4">
        <Button type="button" variant="ghost" onclick={cancelClone} disabled={cloning}>
          Cancel
        </Button>
        <Button type="submit" disabled={cloning || !cloneName.trim()}>
          {cloning ? 'Cloning…' : 'Clone'}
        </Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
