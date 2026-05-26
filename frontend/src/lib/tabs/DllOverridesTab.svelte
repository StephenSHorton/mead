<script lang="ts">
  import type { main } from '../../../wailsjs/go/models'
  import { GetEnv, SetDLLOverride } from '../../../wailsjs/go/main/App.js'
  import { errMessage } from '../format'
  import { ui } from '../store.svelte'

  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Alert, AlertDescription } from '$lib/components/ui/alert'
  import { Badge } from '$lib/components/ui/badge'
  import { toast } from 'svelte-sonner'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import Trash2Icon from '@lucide/svelte/icons/trash-2'
  import AlertCircleIcon from '@lucide/svelte/icons/alert-circle'
  import LayersIcon from '@lucide/svelte/icons/layers'

  let { bottle }: { bottle: main.BottleSummary } = $props()

  // The valid mode strings the Go side understands. "" removes.
  const MODES = [
    'native',
    'builtin',
    'native,builtin',
    'builtin,native',
    'disabled',
  ] as const
  type Mode = (typeof MODES)[number]

  let raw = $state('')
  let loading = $state(true)
  let loadError = $state('')

  let newDll = $state('')
  let newMode = $state<Mode>('native,builtin')
  let saving = $state(false)
  let saveError = $state('')

  let dllInput = $state<HTMLInputElement | null>(null)
  let lastNonce = $state(0)

  $effect(() => {
    if (ui.activeTab === 'dll' && ui.focusNonce !== lastNonce) {
      lastNonce = ui.focusNonce
      queueMicrotask(() => dllInput?.focus())
    }
  })

  // Parse a WINEDLLOVERRIDES string into a list of (dll, mode) pairs.
  // The format is `dllA,dllB=mode1;dllC=mode2`. A bare entry (no `=`) is
  // treated as `=` (disabled). Multiple DLLs can share a mode via commas
  // before the `=`.
  function parse(s: string): { dll: string; mode: string }[] {
    if (!s) return []
    const out: { dll: string; mode: string }[] = []
    for (const segRaw of s.split(';')) {
      const seg = segRaw.trim()
      if (!seg) continue
      const eq = seg.indexOf('=')
      let dlls: string[]
      let mode: string
      if (eq < 0) {
        dlls = seg.split(',').map((d) => d.trim()).filter(Boolean)
        mode = '' // empty = disabled in Wine semantics
      } else {
        dlls = seg
          .slice(0, eq)
          .split(',')
          .map((d) => d.trim())
          .filter(Boolean)
        mode = seg.slice(eq + 1).trim()
      }
      for (const d of dlls) out.push({ dll: d, mode })
    }
    return out
  }

  let entries = $derived(parse(raw))

  async function refresh() {
    try {
      const env = (await GetEnv(bottle.id)) || {}
      raw = env['WINEDLLOVERRIDES'] || ''
      loadError = ''
    } catch (err) {
      loadError = errMessage(err)
    } finally {
      loading = false
    }
  }

  $effect(() => {
    const id = bottle.id
    let cancelled = false
    ;(async () => {
      loading = true
      try {
        const env = (await GetEnv(id)) || {}
        if (cancelled) return
        raw = env['WINEDLLOVERRIDES'] || ''
        loadError = ''
      } catch (err) {
        if (cancelled) return
        loadError = errMessage(err)
      } finally {
        if (!cancelled) loading = false
      }
    })()
    return () => {
      cancelled = true
    }
  })

  async function add(e: SubmitEvent) {
    e.preventDefault()
    const d = newDll.trim()
    if (!d || saving) return
    saving = true
    saveError = ''
    try {
      await SetDLLOverride(bottle.id, d, newMode)
      newDll = ''
      toast.success('Override set', { description: `${d} = ${newMode}` })
      await refresh()
    } catch (err) {
      saveError = errMessage(err)
    } finally {
      saving = false
    }
  }

  async function remove(dll: string) {
    try {
      await SetDLLOverride(bottle.id, dll, '')
      toast.success('Override removed', { description: dll })
      await refresh()
    } catch (err) {
      toast.error('Remove failed', { description: errMessage(err) })
    }
  }

  function modeLabel(mode: string): string {
    return mode === '' ? 'disabled (empty)' : mode
  }
</script>

<div class="space-y-4">
  <form class="bg-card rounded-lg border p-4" onsubmit={add}>
    <div class="mb-2 flex items-center gap-2">
      <PlusIcon class="text-muted-foreground size-4" />
      <h3 class="text-sm font-medium">Add DLL override</h3>
    </div>
    <p class="text-muted-foreground mb-3 text-xs">
      Tells Wine which DLL implementation to load — native (Windows) or builtin (Wine's reimplementation).
    </p>
    <div class="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_auto_auto]">
      <div>
        <Label for="dll-name-{bottle.id}" class="text-xs">DLL name <span class="text-muted-foreground">(no .dll)</span></Label>
        <Input
          id="dll-name-{bottle.id}"
          bind:ref={dllInput}
          bind:value={newDll}
          placeholder="e.g. d3d11"
          class="mt-1 font-mono text-xs"
          disabled={saving}
        />
      </div>
      <div>
        <Label for="dll-mode-{bottle.id}" class="text-xs">Mode</Label>
        <select
          id="dll-mode-{bottle.id}"
          bind:value={newMode}
          class="border-input bg-background text-foreground focus-visible:ring-ring/50 mt-1 h-8 rounded-md border px-2 text-xs focus-visible:ring-3 focus-visible:outline-none"
          disabled={saving}
        >
          {#each MODES as m}
            <option value={m}>{m}</option>
          {/each}
        </select>
      </div>
      <div class="flex items-end">
        <Button type="submit" size="sm" disabled={saving || !newDll.trim()}>
          {saving ? 'Saving…' : 'Set'}
        </Button>
      </div>
    </div>
    {#if saveError}
      <Alert variant="destructive" class="mt-2">
        <AlertCircleIcon />
        <AlertDescription>{saveError}</AlertDescription>
      </Alert>
    {/if}
  </form>

  {#if loadError}
    <Alert variant="destructive">
      <AlertCircleIcon />
      <AlertDescription>{loadError}</AlertDescription>
    </Alert>
  {/if}

  {#if loading}
    <p class="text-muted-foreground text-xs">Loading…</p>
  {:else if entries.length === 0}
    <div class="text-muted-foreground bg-card flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed p-10 text-center">
      <LayersIcon class="text-muted-foreground/60 size-8" />
      <p class="text-sm">No DLL overrides set</p>
      <p class="text-xs">
        Add an entry above. Common ones: <code class="bg-muted rounded px-1">d3d11=native</code>,
        <code class="bg-muted rounded px-1">dxgi=builtin</code>.
      </p>
    </div>
  {:else}
    <ul class="divide-border/50 bg-card divide-y rounded-md border">
      {#each entries as e (e.dll)}
        <li class="flex items-center gap-3 px-3 py-2">
          <code class="text-foreground/90 truncate font-mono text-xs font-medium">{e.dll}</code>
          <Badge variant="secondary" class="font-mono text-[0.65rem]">
            {modeLabel(e.mode)}
          </Badge>
          <div class="ml-auto">
            <Button
              variant="ghost"
              size="icon-sm"
              class="text-muted-foreground hover:text-destructive"
              aria-label="Remove override"
              onclick={() => remove(e.dll)}
            >
              <Trash2Icon />
            </Button>
          </div>
        </li>
      {/each}
    </ul>
    <p class="text-muted-foreground font-mono text-[0.65rem]">
      raw: WINEDLLOVERRIDES={raw}
    </p>
  {/if}
</div>
