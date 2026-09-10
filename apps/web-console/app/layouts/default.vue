<script setup lang="ts">
const sidebarCollapsed = ref(false)

const sidebarToggleLabel = computed(() => (
  sidebarCollapsed.value ? 'Expand sidebar' : 'Collapse sidebar'
))

const toggleSidebar = () => {
  sidebarCollapsed.value = !sidebarCollapsed.value
}
</script>

<template>
  <div class="app-shell">
    <a
      class="skip-link"
      href="#main-content"
    >Skip to main content</a>

    <aside
      class="app-sidebar"
      :class="{ 'app-sidebar--collapsed': sidebarCollapsed }"
      aria-label="Primary navigation"
    >
      <div class="app-sidebar-header">
        <NuxtLink
          v-if="!sidebarCollapsed"
          class="brand"
          to="/"
          aria-label="PulseGrid home"
        >
          <span class="brand-wordmark">PulseGrid</span>
        </NuxtLink>
        <button
          class="sidebar-toggle"
          type="button"
          aria-controls="primary-navigation"
          :aria-expanded="!sidebarCollapsed"
          :aria-label="sidebarToggleLabel"
          :title="sidebarToggleLabel"
          @click="toggleSidebar"
        >
          <UIcon
            :name="sidebarCollapsed ? 'i-lucide-panel-left-open' : 'i-lucide-panel-left-close'"
            class="size-5"
            aria-hidden="true"
          />
        </button>
      </div>

      <nav
        id="primary-navigation"
        class="app-nav"
        aria-label="Operations console"
      >
        <NuxtLink
          class="app-nav-link"
          to="/"
          aria-current="page"
          :title="sidebarCollapsed ? 'Overview' : undefined"
        >
          <UIcon
            name="i-lucide-house"
            class="size-5"
            aria-hidden="true"
          />
          <span class="nav-label">Overview</span>
        </NuxtLink>
      </nav>
    </aside>

    <div class="app-main">
      <header
        class="app-mobile-header"
        aria-label="Mobile navigation"
      >
        <NuxtLink
          class="brand"
          to="/"
          aria-label="PulseGrid home"
        >PulseGrid</NuxtLink>
        <span
          class="mobile-nav-state"
          aria-current="page"
        >Overview</span>
      </header>

      <main
        id="main-content"
        class="app-content"
      >
        <slot />
      </main>
    </div>
  </div>
</template>
