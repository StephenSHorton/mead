<script lang="ts">
  import type { main } from '../../../wailsjs/go/models'
  import * as Command from '$lib/components/ui/command'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import WineIcon from '@lucide/svelte/icons/wine'
  import DownloadIcon from '@lucide/svelte/icons/download'
  import PlayIcon from '@lucide/svelte/icons/play'
  import SlidersIcon from '@lucide/svelte/icons/sliders-horizontal'
  import LayersIcon from '@lucide/svelte/icons/layers'
  import FlaskConicalIcon from '@lucide/svelte/icons/flask-conical'
  import Trash2Icon from '@lucide/svelte/icons/trash-2'
  import TerminalIcon from '@lucide/svelte/icons/terminal'
  import HelpCircleIcon from '@lucide/svelte/icons/help-circle'
  import { ui } from '../store.svelte'

  let {
    bottles,
    onNewBottle,
    onDeleteBottle,
  }: {
    bottles: main.BottleSummary[]
    onNewBottle: () => void
    onDeleteBottle: (id: string) => void
  } = $props()

  // Global hotkey: Cmd+K (macOS) / Ctrl+K (others) toggles the palette.
  // We attach a single window keydown listener via $effect.
  $effect(() => {
    function onKey(e: KeyboardEvent) {
      const isK = e.key === 'k' || e.key === 'K'
      if (isK && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        ui.togglePalette()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  function run(fn: () => void) {
    ui.paletteOpen = false
    // Defer so the dialog can close before any new dialog/tab effect runs.
    queueMicrotask(fn)
  }

  let selectedBottle = $derived(
    bottles.find((b) => b.id === ui.selectedBottleId)
  )
</script>

<Command.Dialog bind:open={ui.paletteOpen}>
  <Command.Input placeholder="Type a command or search bottles…" />
  <Command.List>
    <Command.Empty>No results.</Command.Empty>

    <Command.Group heading="Bottles">
      <Command.Item
        keywords={['create', 'new', 'add']}
        onSelect={() => run(onNewBottle)}
      >
        <PlusIcon />
        <span>Create bottle</span>
      </Command.Item>
      {#each bottles as b (b.id)}
        <Command.Item
          keywords={[b.name, 'select', 'switch', 'open']}
          onSelect={() => run(() => ui.selectBottle(b.id))}
        >
          <WineIcon />
          <span>Select <b>{b.name}</b></span>
          {#if b.wine_version}
            <Command.Shortcut>{b.wine_version}</Command.Shortcut>
          {/if}
        </Command.Item>
      {/each}
    </Command.Group>

    {#if selectedBottle}
      <Command.Separator />
      <Command.Group heading="This bottle ({selectedBottle.name})">
        <Command.Item
          keywords={['processes', 'logs']}
          onSelect={() => run(() => ui.openTab('processes'))}
        >
          <TerminalIcon />
          <span>View processes</span>
        </Command.Item>
        <Command.Item
          keywords={['install', 'app', 'exe', 'setup']}
          onSelect={() => run(() => ui.openTab('apps', true))}
        >
          <DownloadIcon />
          <span>Install app</span>
        </Command.Item>
        <Command.Item
          keywords={['launch', 'run', 'open']}
          onSelect={() => run(() => ui.openTab('apps', true))}
        >
          <PlayIcon />
          <span>Launch app</span>
        </Command.Item>
        <Command.Item
          keywords={['winetricks', 'verb', 'dotnet', 'vcrun']}
          onSelect={() => run(() => ui.openTab('winetricks', true))}
        >
          <FlaskConicalIcon />
          <span>Run winetricks verb</span>
        </Command.Item>
        <Command.Item
          keywords={['env', 'environment', 'variable']}
          onSelect={() => run(() => ui.openTab('env', true))}
        >
          <SlidersIcon />
          <span>Set env variable</span>
        </Command.Item>
        <Command.Item
          keywords={['dll', 'override', 'winedlloverrides']}
          onSelect={() => run(() => ui.openTab('dll', true))}
        >
          <LayersIcon />
          <span>Set DLL override</span>
        </Command.Item>
        <Command.Item
          keywords={['delete', 'remove']}
          onSelect={() => run(() => onDeleteBottle(selectedBottle!.id))}
        >
          <Trash2Icon />
          <span>Delete <b>{selectedBottle.name}</b></span>
        </Command.Item>
      </Command.Group>
    {/if}

    <Command.Separator />
    <Command.Group heading="Help">
      <Command.Item disabled keywords={['docs', 'help']}>
        <HelpCircleIcon />
        <span>Documentation (coming soon)</span>
      </Command.Item>
    </Command.Group>
  </Command.List>
</Command.Dialog>
