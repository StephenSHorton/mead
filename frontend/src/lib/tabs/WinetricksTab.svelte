<script lang="ts">
  import type { main } from '../../../wailsjs/go/models'
  import { RunWinetricks } from '../../../wailsjs/go/main/App.js'
  import { errMessage } from '../format'
  import { ui } from '../store.svelte'

  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Alert, AlertDescription } from '$lib/components/ui/alert'
  import { Badge } from '$lib/components/ui/badge'
  import { toast } from 'svelte-sonner'
  import PlayIcon from '@lucide/svelte/icons/play'
  import FlaskConicalIcon from '@lucide/svelte/icons/flask-conical'
  import AlertCircleIcon from '@lucide/svelte/icons/alert-circle'

  let { bottle }: { bottle: main.BottleSummary } = $props()

  // Curated suggestions — the actual input is free-form so any verb works.
  const SUGGESTIONS = [
    'd3dx9',
    'd3dx11_43',
    'dotnet48',
    'vcrun2022',
    'vcrun2019',
    'corefonts',
    'dxvk',
    'mf',
  ]

  let verb = $state('')
  let running = $state(false)
  let runError = $state('')

  let verbInput = $state<HTMLInputElement | null>(null)
  let lastNonce = $state(0)

  $effect(() => {
    if (ui.activeTab === 'winetricks' && ui.focusNonce !== lastNonce) {
      lastNonce = ui.focusNonce
      queueMicrotask(() => verbInput?.focus())
    }
  })

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    const v = verb.trim()
    if (!v || running) return
    running = true
    runError = ''
    try {
      const runID = await RunWinetricks(bottle.id, v)
      toast.success('Winetricks started', {
        description: `${v} — run ${runID.slice(0, 8)} (see Processes)`,
      })
      verb = ''
      ui.openTab('processes')
    } catch (err) {
      runError = errMessage(err)
    } finally {
      running = false
    }
  }

  function applySuggestion(s: string) {
    verb = s
    queueMicrotask(() => verbInput?.focus())
  }
</script>

<div class="space-y-4">
  <form class="bg-card rounded-lg border p-4" onsubmit={submit}>
    <div class="mb-2 flex items-center gap-2">
      <FlaskConicalIcon class="text-muted-foreground size-4" />
      <h3 class="text-sm font-medium">Run a winetricks verb</h3>
    </div>
    <p class="text-muted-foreground mb-3 text-xs">
      Installs a Windows runtime or library into this bottle. Surface the running process in the Processes tab.
    </p>
    <Label for="wt-verb-{bottle.id}" class="text-xs">Verb</Label>
    <div class="mt-1 flex flex-wrap gap-2">
      <Input
        id="wt-verb-{bottle.id}"
        bind:ref={verbInput}
        bind:value={verb}
        placeholder="e.g. dotnet48"
        class="flex-1 font-mono text-xs"
        disabled={running}
      />
      <Button type="submit" size="sm" disabled={running || !verb.trim()}>
        <PlayIcon />
        {running ? 'Starting…' : 'Run'}
      </Button>
    </div>
    {#if runError}
      <Alert variant="destructive" class="mt-2">
        <AlertCircleIcon />
        <AlertDescription>{runError}</AlertDescription>
      </Alert>
    {/if}
  </form>

  <div>
    <Label class="text-xs">Common verbs</Label>
    <div class="mt-2 flex flex-wrap gap-1.5">
      {#each SUGGESTIONS as s}
        <button
          type="button"
          class="cursor-pointer"
          onclick={() => applySuggestion(s)}
          aria-label="Use {s}"
        >
          <Badge variant="outline" class="font-mono text-xs hover:bg-muted">
            {s}
          </Badge>
        </button>
      {/each}
    </div>
  </div>
</div>
