// Shared app state: selected bottle, command-palette open/close, and a
// "tab intent" channel used by the command palette to switch the bottle-
// detail tab (and optionally request focus on a tab's primary input).
//
// We use Svelte 5 module-level $state so any component can `import { ui }`
// and read/mutate reactively.

export type DetailTab = 'processes' | 'apps' | 'env' | 'dll' | 'winetricks'

class UIStore {
  selectedBottleId = $state<string | null>(null)
  paletteOpen = $state(false)
  activeTab = $state<DetailTab>('processes')

  // Monotonically-increasing nonce — components listen via $effect and
  // focus their primary input when it ticks while their tab matches.
  focusNonce = $state(0)

  selectBottle(id: string | null) {
    this.selectedBottleId = id
  }

  toggleBottle(id: string) {
    this.selectedBottleId = this.selectedBottleId === id ? null : id
  }

  openTab(tab: DetailTab, focus = false) {
    this.activeTab = tab
    if (focus) this.focusNonce++
  }

  openPalette() {
    this.paletteOpen = true
  }

  togglePalette() {
    this.paletteOpen = !this.paletteOpen
  }
}

export const ui = new UIStore()
