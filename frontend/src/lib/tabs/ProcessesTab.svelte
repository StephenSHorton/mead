<script lang="ts">
  import type { main } from '../../../wailsjs/go/models'
  import { ListProcesses } from '../../../wailsjs/go/main/App.js'
  import ProcessList from '../ProcessList.svelte'
  import { errMessage } from '../format'

  let { bottle }: { bottle: main.BottleSummary } = $props()

  let processes = $state<main.ProcessSummary[]>([])
  let procsLoading = $state(true)
  let procsError = $state('')

  const POLL_MS = 2000

  // Polling lifecycle — preserved from the original BottleDetail. setTimeout
  // chain (not setInterval) so each tick waits for the previous fetch,
  // avoiding overlapping calls under load. $effect cleanup cancels the
  // pending timer on unmount or when `bottle.id` changes.
  $effect(() => {
    const id = bottle.id
    let cancelled = false
    let timer: ReturnType<typeof setTimeout> | null = null

    async function refresh() {
      try {
        const list = await ListProcesses(id)
        if (cancelled) return
        processes = list || []
        procsError = ''
      } catch (err) {
        if (cancelled) return
        procsError = errMessage(err)
      } finally {
        if (!cancelled) procsLoading = false
      }
    }

    async function loop() {
      if (cancelled) return
      await refresh()
      if (cancelled) return
      timer = setTimeout(loop, POLL_MS)
    }

    procsLoading = true
    processes = []
    procsError = ''
    void loop()

    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  })
</script>

<ProcessList {processes} loading={procsLoading} error={procsError} bottleID={bottle.id} />
