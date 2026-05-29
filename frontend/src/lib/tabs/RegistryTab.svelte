<script lang="ts">
  import type { main } from '../../../wailsjs/go/models'
  import { GetRegistry, SetRegistry } from '../../../wailsjs/go/main/App.js'
  import { errMessage } from '../format'
  import { ui } from '../store.svelte'

  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Alert, AlertDescription } from '$lib/components/ui/alert'
  import { Badge } from '$lib/components/ui/badge'
  import { toast } from 'svelte-sonner'
  import SearchIcon from '@lucide/svelte/icons/search'
  import PencilIcon from '@lucide/svelte/icons/pencil'
  import AlertCircleIcon from '@lucide/svelte/icons/alert-circle'
  import KeyRoundIcon from '@lucide/svelte/icons/key-round'

  let { bottle }: { bottle: main.BottleSummary } = $props()

  // The REG_* types `reg add /t` accepts. REG_SZ is the default.
  const TYPES = [
    'REG_SZ',
    'REG_EXPAND_SZ',
    'REG_DWORD',
    'REG_QWORD',
    'REG_MULTI_SZ',
    'REG_BINARY',
    'REG_NONE',
  ] as const
  type RegType = (typeof TYPES)[number]

  // --- Read ----------------------------------------------------------------
  let queryKey = $state('')
  let queryValue = $state('')
  let querying = $state(false)
  let queryError = $state('')
  let result = $state<main.RegistryQueryResult | null>(null)

  let keyInput = $state<HTMLInputElement | null>(null)
  let lastNonce = $state(0)
  $effect(() => {
    if (ui.activeTab === 'registry' && ui.focusNonce !== lastNonce) {
      lastNonce = ui.focusNonce
      queueMicrotask(() => keyInput?.focus())
    }
  })

  async function submitQuery(e: SubmitEvent) {
    e.preventDefault()
    const k = queryKey.trim()
    if (!k || querying) return
    querying = true
    queryError = ''
    try {
      result = await GetRegistry(bottle.id, k, queryValue.trim())
    } catch (err) {
      result = null
      queryError = errMessage(err)
    } finally {
      querying = false
    }
  }

  // --- Write ---------------------------------------------------------------
  let setKey = $state('')
  let setValue = $state('')
  let setType = $state<RegType>('REG_SZ')
  let setData = $state('')
  let setting = $state(false)
  let setError = $state('')

  async function submitSet(e: SubmitEvent) {
    e.preventDefault()
    const k = setKey.trim()
    if (!k || setting) return
    setting = true
    setError = ''
    try {
      await SetRegistry(bottle.id, k, setValue.trim(), setType, setData)
      const what = setValue.trim() ? `${setValue.trim()} (${setType})` : 'key'
      toast.success('Registry written', { description: `${what} in ${k}` })
      // If the just-written key is what's on screen, refresh the read view.
      if (result && result.key && setKey.trim() === queryKey.trim()) {
        try {
          result = await GetRegistry(bottle.id, queryKey.trim(), queryValue.trim())
        } catch {
          /* non-fatal: the write succeeded regardless */
        }
      }
    } catch (err) {
      setError = errMessage(err)
    } finally {
      setting = false
    }
  }
</script>

<div class="space-y-4">
  <!-- Read panel -->
  <form class="bg-card rounded-lg border p-4" onsubmit={submitQuery}>
    <div class="mb-2 flex items-center gap-2">
      <SearchIcon class="text-muted-foreground size-4" />
      <h3 class="text-sm font-medium">Read a key</h3>
    </div>
    <div class="grid grid-cols-1 gap-2 sm:grid-cols-[2fr_1fr_auto]">
      <div>
        <Label for="reg-q-key-{bottle.id}" class="text-xs">Key path</Label>
        <Input
          id="reg-q-key-{bottle.id}"
          bind:ref={keyInput}
          bind:value={queryKey}
          placeholder={'HKCU\\Software\\Wine\\Direct3D'}
          class="mt-1 font-mono text-xs"
          disabled={querying}
        />
      </div>
      <div>
        <Label for="reg-q-val-{bottle.id}" class="text-xs">Value <span class="text-muted-foreground">(optional)</span></Label>
        <Input
          id="reg-q-val-{bottle.id}"
          bind:value={queryValue}
          placeholder="(whole key)"
          class="mt-1 font-mono text-xs"
          disabled={querying}
        />
      </div>
      <div class="flex items-end">
        <Button type="submit" size="sm" disabled={querying || !queryKey.trim()}>
          {querying ? 'Reading…' : 'Query'}
        </Button>
      </div>
    </div>
    {#if queryError}
      <Alert variant="destructive" class="mt-2">
        <AlertCircleIcon />
        <AlertDescription>{queryError}</AlertDescription>
      </Alert>
    {/if}
  </form>

  <!-- Read result -->
  {#if result}
    <div class="bg-card rounded-md border">
      <div class="border-b px-3 py-2">
        <code class="text-foreground/90 font-mono text-xs font-medium break-all">{result.key}</code>
      </div>
      {#if result.values.length > 0}
        <ul class="divide-border/50 divide-y">
          {#each result.values as v (v.name)}
            <li class="flex flex-wrap items-center gap-2 px-3 py-2 text-xs">
              <code class="text-foreground/90 font-mono font-medium">{v.name || '(Default)'}</code>
              <Badge variant="secondary" class="font-mono text-[0.6rem]">{v.type}</Badge>
              <code class="text-muted-foreground font-mono break-all">{v.data}</code>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="text-muted-foreground px-3 py-2 text-xs">No values on this key.</p>
      {/if}
      {#if result.subkeys && result.subkeys.length > 0}
        <div class="border-t px-3 py-2">
          <p class="text-muted-foreground mb-1 text-[0.65rem] uppercase tracking-wide">Subkeys</p>
          <ul class="space-y-0.5">
            {#each result.subkeys as sk (sk)}
              <li><code class="text-muted-foreground font-mono text-xs break-all">{sk}</code></li>
            {/each}
          </ul>
        </div>
      {/if}
    </div>
  {/if}

  <!-- Write panel -->
  <form class="bg-card rounded-lg border p-4" onsubmit={submitSet}>
    <div class="mb-2 flex items-center gap-2">
      <PencilIcon class="text-muted-foreground size-4" />
      <h3 class="text-sm font-medium">Write a value</h3>
    </div>
    <p class="text-muted-foreground mb-3 text-xs">
      Leave the value name empty to create the key only. <code class="bg-muted rounded px-1">/f</code> is always passed, so an existing value is overwritten without a prompt.
    </p>
    <div class="space-y-2">
      <div>
        <Label for="reg-s-key-{bottle.id}" class="text-xs">Key path</Label>
        <Input
          id="reg-s-key-{bottle.id}"
          bind:value={setKey}
          placeholder={'HKCU\\Software\\Wine\\Direct3D'}
          class="mt-1 font-mono text-xs"
          disabled={setting}
        />
      </div>
      <div class="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_auto_2fr]">
        <div>
          <Label for="reg-s-val-{bottle.id}" class="text-xs">Value name</Label>
          <Input
            id="reg-s-val-{bottle.id}"
            bind:value={setValue}
            placeholder="(Default)"
            class="mt-1 font-mono text-xs"
            disabled={setting}
          />
        </div>
        <div>
          <Label for="reg-s-type-{bottle.id}" class="text-xs">Type</Label>
          <select
            id="reg-s-type-{bottle.id}"
            bind:value={setType}
            class="border-input bg-background text-foreground focus-visible:ring-ring/50 mt-1 h-8 w-full rounded-md border px-2 text-xs focus-visible:ring-3 focus-visible:outline-none"
            disabled={setting || !setValue.trim()}
          >
            {#each TYPES as t}
              <option value={t}>{t}</option>
            {/each}
          </select>
        </div>
        <div>
          <Label for="reg-s-data-{bottle.id}" class="text-xs">Data</Label>
          <Input
            id="reg-s-data-{bottle.id}"
            bind:value={setData}
            placeholder={setType === 'REG_DWORD' ? '1' : 'value'}
            class="mt-1 font-mono text-xs"
            disabled={setting || !setValue.trim()}
          />
        </div>
      </div>
      <div>
        <Button type="submit" size="sm" disabled={setting || !setKey.trim()}>
          {setting ? 'Writing…' : 'Set'}
        </Button>
      </div>
    </div>
    {#if setError}
      <Alert variant="destructive" class="mt-2">
        <AlertCircleIcon />
        <AlertDescription>{setError}</AlertDescription>
      </Alert>
    {/if}
  </form>

  {#if !result && !queryError}
    <div class="text-muted-foreground bg-card flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed p-10 text-center">
      <KeyRoundIcon class="text-muted-foreground/60 size-8" />
      <p class="text-sm">Read or write the bottle's Windows registry</p>
      <p class="text-xs">
        Try <code class="bg-muted rounded px-1">{'HKCU\\Software\\Wine'}</code> to see Wine's own keys.
      </p>
    </div>
  {/if}
</div>
