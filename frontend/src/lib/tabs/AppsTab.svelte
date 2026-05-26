<script lang="ts">
  import type { main } from '../../../wailsjs/go/models'
  import { InstallApp, LaunchApp } from '../../../wailsjs/go/main/App.js'
  import { errMessage } from '../format'
  import { ui } from '../store.svelte'

  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Alert, AlertDescription } from '$lib/components/ui/alert'
  import { toast } from 'svelte-sonner'
  import DownloadIcon from '@lucide/svelte/icons/download'
  import PlayIcon from '@lucide/svelte/icons/play'
  import AlertCircleIcon from '@lucide/svelte/icons/alert-circle'

  let { bottle }: { bottle: main.BottleSummary } = $props()

  let installerPath = $state('')
  let installing = $state(false)
  let installError = $state('')

  let exePath = $state('')
  let launching = $state(false)
  let launchError = $state('')

  let installInput = $state<HTMLInputElement | null>(null)
  let launchInput = $state<HTMLInputElement | null>(null)

  // Listen for focus requests from the command palette. When the active tab
  // is 'apps' and the nonce ticks, focus the installer input as the canonical
  // primary input for this tab.
  let lastNonce = $state(0)
  $effect(() => {
    if (ui.activeTab === 'apps' && ui.focusNonce !== lastNonce) {
      lastNonce = ui.focusNonce
      // Defer to next microtask so the input is mounted.
      queueMicrotask(() => installInput?.focus())
    }
  })

  async function submitInstall(e: SubmitEvent) {
    e.preventDefault()
    const p = installerPath.trim()
    if (!p || installing) return
    installing = true
    installError = ''
    try {
      const runID = await InstallApp(bottle.id, p, [])
      installerPath = ''
      toast.success('Installer started', {
        description: `run ${runID.slice(0, 8)} — see Processes`,
      })
      ui.openTab('processes')
    } catch (err) {
      installError = errMessage(err)
    } finally {
      installing = false
    }
  }

  async function submitLaunch(e: SubmitEvent) {
    e.preventDefault()
    const p = exePath.trim()
    if (!p || launching) return
    launching = true
    launchError = ''
    try {
      const runID = await LaunchApp(bottle.id, p)
      exePath = ''
      toast.success('Launched', {
        description: `run ${runID.slice(0, 8)} — see Processes`,
      })
      ui.openTab('processes')
    } catch (err) {
      launchError = errMessage(err)
    } finally {
      launching = false
    }
  }
</script>

<div class="space-y-4">
  <!-- Install panel -->
  <form class="bg-card rounded-lg border p-4" onsubmit={submitInstall}>
    <div class="mb-2 flex items-center gap-2">
      <DownloadIcon class="text-muted-foreground size-4" />
      <h3 class="text-sm font-medium">Run installer</h3>
    </div>
    <Label for="installer-path-{bottle.id}" class="text-xs">
      Installer path on this Mac
      <span class="text-muted-foreground">(e.g.</span>
      <code class="bg-muted rounded px-1 text-[0.7rem]">/Users/you/Downloads/setup.exe</code><span class="text-muted-foreground">)</span>
    </Label>
    <div class="mt-2 flex flex-wrap gap-2">
      <Input
        id="installer-path-{bottle.id}"
        bind:ref={installInput}
        placeholder="/path/to/installer.exe"
        class="flex-1 font-mono text-xs"
        bind:value={installerPath}
        disabled={installing}
      />
      <Button type="submit" size="sm" disabled={installing || !installerPath.trim()}>
        {installing ? 'Running…' : 'Install'}
      </Button>
    </div>
    {#if installError}
      <Alert variant="destructive" class="mt-2">
        <AlertCircleIcon />
        <AlertDescription>{installError}</AlertDescription>
      </Alert>
    {/if}
  </form>

  <!-- Launch panel -->
  <form class="bg-card rounded-lg border p-4" onsubmit={submitLaunch}>
    <div class="mb-2 flex items-center gap-2">
      <PlayIcon class="text-muted-foreground size-4" />
      <h3 class="text-sm font-medium">Launch app</h3>
    </div>
    <Label for="exe-path-{bottle.id}" class="text-xs">
      Executable path
      <span class="text-muted-foreground">(relative to</span>
      <code class="bg-muted rounded px-1 text-[0.7rem]">drive_c</code>
      <span class="text-muted-foreground">or absolute)</span>
    </Label>
    <div class="mt-2 flex flex-wrap gap-2">
      <Input
        id="exe-path-{bottle.id}"
        bind:ref={launchInput}
        placeholder="Program Files/MyApp/app.exe"
        class="flex-1 font-mono text-xs"
        bind:value={exePath}
        disabled={launching}
      />
      <Button type="submit" size="sm" disabled={launching || !exePath.trim()}>
        {launching ? 'Launching…' : 'Launch'}
      </Button>
    </div>
    {#if launchError}
      <Alert variant="destructive" class="mt-2">
        <AlertCircleIcon />
        <AlertDescription>{launchError}</AlertDescription>
      </Alert>
    {/if}
  </form>
</div>
