<script lang="ts">
  import type { main } from '../../../wailsjs/go/models'
  import * as Sidebar from '$lib/components/ui/sidebar'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Badge } from '$lib/components/ui/badge'
  import { toast } from 'svelte-sonner'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import Trash2Icon from '@lucide/svelte/icons/trash-2'
  import WineIcon from '@lucide/svelte/icons/wine'
  import FlaskConicalIcon from '@lucide/svelte/icons/flask-conical'

  import { CreateBottle } from '../../../wailsjs/go/main/App.js'
  import { errMessage } from '../format'
  import { ui } from '../store.svelte'

  let {
    bottles,
    loading,
    onAskDelete,
  }: {
    bottles: main.BottleSummary[]
    loading: boolean
    onAskDelete: (id: string) => void
  } = $props()

  let showCreate = $state(false)
  let newName = $state('')
  let creating = $state(false)

  function openCreate() {
    newName = ''
    showCreate = true
  }

  // The command palette's "Create bottle" action dispatches this event so we
  // pop open the inline form. Keeps a single source of truth for the form.
  $effect(() => {
    function onNew() {
      openCreate()
    }
    window.addEventListener('mead:new-bottle', onNew)
    return () => window.removeEventListener('mead:new-bottle', onNew)
  })

  async function submitCreate(e: SubmitEvent) {
    e.preventDefault()
    const name = newName.trim()
    if (!name || creating) return
    creating = true
    const toastId = toast.loading('Creating bottle…', {
      description: 'running wineboot — this takes 5–15s',
    })
    try {
      const b = await CreateBottle(name)
      toast.success('Bottle created', { id: toastId, description: name })
      showCreate = false
      newName = ''
      // Auto-select the newly created bottle.
      if (b && b.id) ui.selectBottle(b.id)
    } catch (err) {
      toast.error('Create failed', { id: toastId, description: errMessage(err) })
    } finally {
      creating = false
    }
  }
</script>

<Sidebar.Root collapsible="icon">
  <Sidebar.Header>
    <div class="flex items-center gap-2 px-2 py-1.5 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
      <div class="bg-sidebar-primary text-sidebar-primary-foreground flex size-7 shrink-0 items-center justify-center rounded-md">
        <FlaskConicalIcon class="size-4" />
      </div>
      <div class="flex min-w-0 flex-col group-data-[collapsible=icon]:hidden">
        <span class="text-sm font-semibold leading-none">Mead</span>
        <span class="text-muted-foreground truncate text-[0.65rem]">Wine for macOS</span>
      </div>
    </div>
  </Sidebar.Header>

  <Sidebar.Content>
    <Sidebar.Group>
      <Sidebar.GroupLabel>Bottles</Sidebar.GroupLabel>
      <Sidebar.GroupAction title="New bottle" onclick={openCreate}>
        <PlusIcon />
        <span class="sr-only">New bottle</span>
      </Sidebar.GroupAction>
      <Sidebar.GroupContent>
        {#if showCreate}
          <form
            class="bg-card mx-2 mb-2 rounded-md border p-2 group-data-[collapsible=icon]:hidden"
            onsubmit={submitCreate}
          >
            <Label for="sb-bottle-name" class="text-[0.65rem]">New bottle name</Label>
            <div class="mt-1.5 flex flex-col gap-1.5">
              <!-- svelte-ignore a11y_autofocus -->
              <Input
                id="sb-bottle-name"
                placeholder="e.g. games"
                class="h-8 text-xs"
                bind:value={newName}
                disabled={creating}
                autofocus
              />
              <div class="flex gap-1.5">
                <Button type="submit" size="sm" class="h-7 flex-1 text-xs" disabled={creating || !newName.trim()}>
                  {creating ? 'Creating…' : 'Create'}
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  class="h-7 text-xs"
                  onclick={() => {
                    showCreate = false
                    newName = ''
                  }}
                  disabled={creating}
                >
                  Cancel
                </Button>
              </div>
            </div>
          </form>
        {/if}

        <Sidebar.Menu>
          {#if loading}
            <Sidebar.MenuItem>
              <div class="text-muted-foreground px-2 py-1 text-xs group-data-[collapsible=icon]:hidden">
                Loading…
              </div>
            </Sidebar.MenuItem>
          {:else if bottles.length === 0}
            <Sidebar.MenuItem>
              <div class="text-muted-foreground px-2 py-1 text-xs group-data-[collapsible=icon]:hidden">
                No bottles yet.
              </div>
            </Sidebar.MenuItem>
          {:else}
            {#each bottles as b (b.id)}
              <Sidebar.MenuItem>
                <Sidebar.MenuButton
                  isActive={ui.selectedBottleId === b.id}
                  tooltipContent={b.name}
                  onclick={() => ui.selectBottle(b.id)}
                >
                  <WineIcon />
                  <span class="truncate">{b.name}</span>
                  {#if b.wine_version}
                    <Badge variant="outline" class="ml-auto font-mono text-[0.6rem] group-data-[collapsible=icon]:hidden">
                      {b.wine_version}
                    </Badge>
                  {/if}
                </Sidebar.MenuButton>
                <Sidebar.MenuAction
                  title="Delete bottle"
                  showOnHover
                  onclick={(e: MouseEvent) => {
                    e.stopPropagation()
                    onAskDelete(b.id)
                  }}
                >
                  <Trash2Icon />
                  <span class="sr-only">Delete bottle</span>
                </Sidebar.MenuAction>
              </Sidebar.MenuItem>
            {/each}
          {/if}
        </Sidebar.Menu>
      </Sidebar.GroupContent>
    </Sidebar.Group>
  </Sidebar.Content>

  <Sidebar.Footer>
    <Button
      variant="outline"
      size="sm"
      class="w-full justify-start group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0"
      onclick={openCreate}
    >
      <PlusIcon />
      <span class="group-data-[collapsible=icon]:hidden">New bottle</span>
    </Button>
  </Sidebar.Footer>

  <Sidebar.Rail />
</Sidebar.Root>
