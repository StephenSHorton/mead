<script lang="ts">
  import type { main } from '../../wailsjs/go/models'
  import LogViewer from './LogViewer.svelte'
  import { fmtRelative } from './format'
  import { ui } from './store.svelte'

  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Alert, AlertDescription } from '$lib/components/ui/alert'
  import TerminalIcon from '@lucide/svelte/icons/terminal'
  import AlertCircleIcon from '@lucide/svelte/icons/alert-circle'
  import PackageOpenIcon from '@lucide/svelte/icons/package-open'

  let {
    processes = [],
    loading = false,
    error = '',
    bottleID = '',
  }: {
    processes?: main.ProcessSummary[]
    loading?: boolean
    error?: string
    bottleID?: string
  } = $props()

  let openRunID = $state<string | null>(null)

  function toggleLogs(runID: string) {
    openRunID = openRunID === runID ? null : runID
  }

  function joinArgv(argv: string[] | undefined): string {
    if (!argv || argv.length === 0) return '(unknown command)'
    return argv.join(' ')
  }

  // Returns the badge variant for an exited process. code=0 is "ok" (secondary),
  // any non-zero code or a run_err implies failure (destructive). The Go side
  // surfaces signal kills as a populated run_err (and may set exit_code to -1).
  function exitVariant(p: main.ProcessSummary): 'secondary' | 'destructive' {
    if (p.run_err) return 'destructive'
    if (p.exit_code === undefined) return 'secondary'
    return p.exit_code === 0 ? 'secondary' : 'destructive'
  }
</script>

<div>
  {#if error}
    <Alert variant="destructive" class="mb-2">
      <AlertCircleIcon />
      <AlertDescription>{error}</AlertDescription>
    </Alert>
  {/if}

  {#if loading && processes.length === 0}
    <p class="text-muted-foreground text-xs">Loading…</p>
  {:else if processes.length === 0}
    <div class="text-muted-foreground bg-card flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed p-10 text-center">
      <PackageOpenIcon class="text-muted-foreground/60 size-8" />
      <p class="text-sm">Nothing's been run in this bottle yet</p>
      <p class="text-xs">Install an app or launch one to get going.</p>
      <Button
        size="sm"
        variant="outline"
        class="mt-2"
        onclick={() => ui.openTab('apps', true)}
        disabled={!bottleID}
      >
        Install an app
      </Button>
    </div>
  {:else}
    <ul class="divide-y divide-border/50 bg-card rounded-md border">
      {#each processes as p (p.run_id)}
        <li class="p-2.5 {openRunID === p.run_id ? 'bg-muted/30' : ''}">
          <div class="flex flex-wrap items-center gap-2">
            <div class="min-w-0 flex-1">
              <code
                class="bg-background block truncate rounded px-1.5 py-0.5 text-xs"
                title={joinArgv(p.argv)}
              >
                {joinArgv(p.argv)}
              </code>
              <div class="text-muted-foreground mt-1 flex items-center gap-2 text-[0.7rem]">
                <span>{fmtRelative(p.started_at)}</span>
                {#if p.exited}
                  <Badge variant={exitVariant(p)} class="text-[0.65rem]">
                    Exited{p.exit_code !== undefined ? ` · code ${p.exit_code}` : ''}
                  </Badge>
                {:else}
                  <Badge class="text-[0.65rem]">Running</Badge>
                {/if}
                {#if p.run_err}
                  <Badge variant="destructive" class="text-[0.65rem]" title={p.run_err}>
                    {p.exited ? 'killed' : 'error'}
                  </Badge>
                {/if}
              </div>
            </div>
            <Button variant="outline" size="xs" onclick={() => toggleLogs(p.run_id)}>
              <TerminalIcon />
              {openRunID === p.run_id ? 'Hide logs' : 'Logs'}
            </Button>
          </div>
          {#if openRunID === p.run_id}
            {#key p.run_id}
              <LogViewer runID={p.run_id} initialExited={p.exited} exitCode={p.exit_code} />
            {/key}
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</div>
