<script lang="ts">
  import type { main } from '../../../wailsjs/go/models'
  import { InstallApp, LaunchApp, UninstallApp } from '../../../wailsjs/go/main/App.js'
  import { errMessage } from '../format'
  import { ui } from '../store.svelte'

  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Alert, AlertDescription } from '$lib/components/ui/alert'
  import { toast } from 'svelte-sonner'
  import DownloadIcon from '@lucide/svelte/icons/download'
  import PlayIcon from '@lucide/svelte/icons/play'
  import Trash2Icon from '@lucide/svelte/icons/trash-2'
  import AlertCircleIcon from '@lucide/svelte/icons/alert-circle'

  let { bottle }: { bottle: main.BottleSummary } = $props()

  let installerPath = $state('')
  let installArgs = $state('')
  let installing = $state(false)
  let installError = $state('')

  let exePath = $state('')
  let launchArgs = $state('')
  let launching = $state(false)
  let launchError = $state('')

  let uninstallKey = $state('')
  let uninstalling = $state(false)
  let uninstallError = $state('')

  let installInput = $state<HTMLInputElement | null>(null)

  // Split a space-separated argument string into argv. No shell quoting —
  // this covers flag-style args (CEF flags like --use-gl=swiftshader,
  // silent-install flags like /S /VERYSILENT), which is the real need.
  function parseArgs(s: string): string[] {
    return s.trim().split(/\s+/).filter(Boolean)
  }

  // Focus the installer input when the palette switches to this tab.
  let lastNonce = $state(0)
  $effect(() => {
    if (ui.activeTab === 'apps' && ui.focusNonce !== lastNonce) {
      lastNonce = ui.focusNonce
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
      const runID = await InstallApp(bottle.id, p, parseArgs(installArgs))
      installerPath = ''
      installArgs = ''
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
      const runID = await LaunchApp(bottle.id, p, parseArgs(launchArgs))
      exePath = ''
      launchArgs = ''
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

  async function submitUninstall(e: SubmitEvent) {
    e.preventDefault()
    const k = uninstallKey.trim()
    if (!k || uninstalling) return
    uninstalling = true
    uninstallError = ''
    try {
      const runID = await UninstallApp(bottle.id, k)
      uninstallKey = ''
      toast.success('Uninstaller started', {
        description: `run ${runID.slice(0, 8)} — see Processes (verify removal)`,
      })
      ui.openTab('processes')
    } catch (err) {
      uninstallError = errMessage(err)
    } finally {
      uninstalling = false
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
    <Input
      placeholder="extra args (optional) — e.g. /S /VERYSILENT"
      class="mt-2 font-mono text-xs"
      bind:value={installArgs}
      disabled={installing}
      aria-label="Installer arguments"
    />
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
        placeholder="Program Files/MyApp/app.exe"
        class="flex-1 font-mono text-xs"
        bind:value={exePath}
        disabled={launching}
      />
      <Button type="submit" size="sm" disabled={launching || !exePath.trim()}>
        {launching ? 'Launching…' : 'Launch'}
      </Button>
    </div>
    <Input
      placeholder="extra args (optional) — e.g. --in-process-gpu --use-gl=swiftshader"
      class="mt-2 font-mono text-xs"
      bind:value={launchArgs}
      disabled={launching}
      aria-label="Launch arguments"
    />
    {#if launchError}
      <Alert variant="destructive" class="mt-2">
        <AlertCircleIcon />
        <AlertDescription>{launchError}</AlertDescription>
      </Alert>
    {/if}
  </form>

  <!-- Uninstall panel -->
  <form class="bg-card rounded-lg border p-4" onsubmit={submitUninstall}>
    <div class="mb-2 flex items-center gap-2">
      <Trash2Icon class="text-muted-foreground size-4" />
      <h3 class="text-sm font-medium">Uninstall a program</h3>
    </div>
    <p class="text-muted-foreground mb-2 text-xs">
      Runs <code class="bg-muted rounded px-1 text-[0.7rem]">wine uninstaller --remove &lt;key&gt;</code>.
      The key is a program's display name or product GUID from
      <code class="bg-muted rounded px-1 text-[0.7rem]">wine uninstaller --list</code>. The uninstaller
      may open its own window — watch Processes and confirm it's gone.
    </p>
    <div class="flex flex-wrap gap-2">
      <Input
        placeholder={'Battle.net  or  {d8bbe9f9-…}'}
        class="flex-1 font-mono text-xs"
        bind:value={uninstallKey}
        disabled={uninstalling}
        aria-label="Uninstaller key"
      />
      <Button type="submit" variant="destructive" size="sm" disabled={uninstalling || !uninstallKey.trim()}>
        {uninstalling ? 'Removing…' : 'Uninstall'}
      </Button>
    </div>
    {#if uninstallError}
      <Alert variant="destructive" class="mt-2">
        <AlertCircleIcon />
        <AlertDescription>{uninstallError}</AlertDescription>
      </Alert>
    {/if}
  </form>
</div>
