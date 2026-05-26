<script lang="ts">
  import type { main } from '../../wailsjs/go/models'
  import { fmtDate } from './format'
  import { ui } from './store.svelte'

  import * as Tabs from '$lib/components/ui/tabs'
  import { Badge } from '$lib/components/ui/badge'
  import TerminalIcon from '@lucide/svelte/icons/terminal'
  import AppWindowIcon from '@lucide/svelte/icons/app-window'
  import SlidersIcon from '@lucide/svelte/icons/sliders-horizontal'
  import LayersIcon from '@lucide/svelte/icons/layers'
  import FlaskConicalIcon from '@lucide/svelte/icons/flask-conical'

  import ProcessesTab from './tabs/ProcessesTab.svelte'
  import AppsTab from './tabs/AppsTab.svelte'
  import EnvTab from './tabs/EnvTab.svelte'
  import DllOverridesTab from './tabs/DllOverridesTab.svelte'
  import WinetricksTab from './tabs/WinetricksTab.svelte'

  let { bottle }: { bottle: main.BottleSummary } = $props()
</script>

<div class="flex h-full flex-col gap-4">
  <!-- Header -->
  <div>
    <h2 class="text-2xl font-semibold tracking-tight">{bottle.name}</h2>
    <div class="text-muted-foreground mt-2 flex flex-wrap items-center gap-2 text-xs">
      <Badge variant="outline" class="font-mono">
        <span class="text-muted-foreground">id</span>
        {bottle.id}
      </Badge>
      {#if bottle.wine_version}
        <Badge variant="outline" class="font-mono">
          <span class="text-muted-foreground">wine</span>
          {bottle.wine_version}
        </Badge>
      {/if}
      {#if bottle.created_at}
        <Badge variant="outline">
          <span class="text-muted-foreground">created</span>
          {fmtDate(bottle.created_at)}
        </Badge>
      {/if}
    </div>
  </div>

  <!-- Tabs -->
  <Tabs.Root
    value={ui.activeTab}
    onValueChange={(v) => ui.openTab(v as typeof ui.activeTab)}
    class="flex flex-1 flex-col gap-3"
  >
    <Tabs.List>
      <Tabs.Trigger value="processes">
        <TerminalIcon />
        Processes
      </Tabs.Trigger>
      <Tabs.Trigger value="apps">
        <AppWindowIcon />
        Apps
      </Tabs.Trigger>
      <Tabs.Trigger value="env">
        <SlidersIcon />
        Env
      </Tabs.Trigger>
      <Tabs.Trigger value="dll">
        <LayersIcon />
        DLL overrides
      </Tabs.Trigger>
      <Tabs.Trigger value="winetricks">
        <FlaskConicalIcon />
        Winetricks
      </Tabs.Trigger>
    </Tabs.List>

    <Tabs.Content value="processes" class="flex-1">
      <ProcessesTab {bottle} />
    </Tabs.Content>
    <Tabs.Content value="apps">
      <AppsTab {bottle} />
    </Tabs.Content>
    <Tabs.Content value="env">
      <EnvTab {bottle} />
    </Tabs.Content>
    <Tabs.Content value="dll">
      <DllOverridesTab {bottle} />
    </Tabs.Content>
    <Tabs.Content value="winetricks">
      <WinetricksTab {bottle} />
    </Tabs.Content>
  </Tabs.Root>
</div>
