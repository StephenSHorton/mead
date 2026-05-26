<script lang="ts">
  import FlaskConicalIcon from '@lucide/svelte/icons/flask-conical'
  import CommandIcon from '@lucide/svelte/icons/command'
  import { Button } from '$lib/components/ui/button'
  import { ui } from '../store.svelte'

  let { hasBottles }: { hasBottles: boolean } = $props()
</script>

<div class="text-muted-foreground flex h-full flex-col items-center justify-center gap-3 p-10 text-center">
  <div class="bg-muted/40 flex size-16 items-center justify-center rounded-full">
    <FlaskConicalIcon class="text-muted-foreground/80 size-8" />
  </div>
  <div>
    <h2 class="text-foreground text-lg font-semibold">
      {hasBottles ? 'Select a bottle' : 'No bottles yet'}
    </h2>
    <p class="mt-1 max-w-sm text-sm">
      {hasBottles
        ? 'Pick a bottle from the sidebar, or create a new one.'
        : 'Create your first bottle from the sidebar to install Windows apps.'}
    </p>
  </div>
  <Button variant="outline" size="sm" onclick={() => ui.openPalette()}>
    <CommandIcon />
    Open command palette
    <span class="text-muted-foreground ml-1 font-mono text-[0.65rem]">⌘K</span>
  </Button>
</div>
