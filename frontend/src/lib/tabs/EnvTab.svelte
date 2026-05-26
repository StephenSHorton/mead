<script lang="ts">
  import type { main } from '../../../wailsjs/go/models'
  import { GetEnv, SetEnv } from '../../../wailsjs/go/main/App.js'
  import { errMessage } from '../format'
  import { ui } from '../store.svelte'

  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Alert, AlertDescription } from '$lib/components/ui/alert'
  import { toast } from 'svelte-sonner'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import Trash2Icon from '@lucide/svelte/icons/trash-2'
  import AlertCircleIcon from '@lucide/svelte/icons/alert-circle'
  import SlidersIcon from '@lucide/svelte/icons/sliders-horizontal'

  let { bottle }: { bottle: main.BottleSummary } = $props()

  // Hide WINEDLLOVERRIDES from this tab — it has a dedicated tab.
  const HIDDEN_KEYS = new Set(['WINEDLLOVERRIDES'])

  let env = $state<Record<string, string>>({})
  let loading = $state(true)
  let loadError = $state('')

  let newKey = $state('')
  let newValue = $state('')
  let saving = $state(false)
  let saveError = $state('')

  let keyInput = $state<HTMLInputElement | null>(null)
  let lastNonce = $state(0)

  $effect(() => {
    if (ui.activeTab === 'env' && ui.focusNonce !== lastNonce) {
      lastNonce = ui.focusNonce
      queueMicrotask(() => keyInput?.focus())
    }
  })

  async function refresh() {
    try {
      env = (await GetEnv(bottle.id)) || {}
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
        const r = (await GetEnv(id)) || {}
        if (cancelled) return
        env = r
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
    const k = newKey.trim()
    if (!k || saving) return
    saving = true
    saveError = ''
    try {
      await SetEnv(bottle.id, k, newValue)
      newKey = ''
      newValue = ''
      toast.success('Env set', { description: k })
      await refresh()
    } catch (err) {
      saveError = errMessage(err)
    } finally {
      saving = false
    }
  }

  async function remove(key: string) {
    try {
      await SetEnv(bottle.id, key, '')
      toast.success('Env removed', { description: key })
      await refresh()
    } catch (err) {
      toast.error('Remove failed', { description: errMessage(err) })
    }
  }

  let visibleEntries = $derived(
    Object.entries(env)
      .filter(([k]) => !HIDDEN_KEYS.has(k))
      .sort(([a], [b]) => a.localeCompare(b))
  )
</script>

<div class="space-y-4">
  <form class="bg-card rounded-lg border p-4" onsubmit={add}>
    <div class="mb-2 flex items-center gap-2">
      <PlusIcon class="text-muted-foreground size-4" />
      <h3 class="text-sm font-medium">Add / update env var</h3>
    </div>
    <div class="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_1fr_auto]">
      <div>
        <Label for="env-key-{bottle.id}" class="text-xs">Key</Label>
        <Input
          id="env-key-{bottle.id}"
          bind:ref={keyInput}
          bind:value={newKey}
          placeholder="e.g. WINEDEBUG"
          class="mt-1 font-mono text-xs"
          disabled={saving}
        />
      </div>
      <div>
        <Label for="env-value-{bottle.id}" class="text-xs">
          Value <span class="text-muted-foreground">(empty removes)</span>
        </Label>
        <Input
          id="env-value-{bottle.id}"
          bind:value={newValue}
          placeholder="e.g. fixme-all"
          class="mt-1 font-mono text-xs"
          disabled={saving}
        />
      </div>
      <div class="flex items-end">
        <Button type="submit" size="sm" disabled={saving || !newKey.trim()}>
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
  {:else if visibleEntries.length === 0}
    <div class="text-muted-foreground bg-card flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed p-10 text-center">
      <SlidersIcon class="text-muted-foreground/60 size-8" />
      <p class="text-sm">No env overrides set</p>
      <p class="text-xs">Add a key above to override Wine's environment for this bottle.</p>
    </div>
  {:else}
    <ul class="divide-border/50 bg-card divide-y rounded-md border">
      {#each visibleEntries as [k, v] (k)}
        <li class="flex items-center gap-3 px-3 py-2">
          <code class="text-foreground/90 truncate font-mono text-xs font-medium">{k}</code>
          <code class="bg-muted/50 text-muted-foreground flex-1 truncate rounded px-1.5 py-0.5 font-mono text-xs" title={v}>
            {v || '(empty)'}
          </code>
          <Button
            variant="ghost"
            size="icon-sm"
            class="text-muted-foreground hover:text-destructive"
            aria-label="Remove env var"
            onclick={() => remove(k)}
          >
            <Trash2Icon />
          </Button>
        </li>
      {/each}
    </ul>
  {/if}
</div>
