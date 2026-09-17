<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const route = useRoute()
const sidebarCollapsed = ref(false)
const mobileMenuOpen = ref(false)
const mobileMenuButton = ref<HTMLButtonElement | null>(null)
const mobileMenuPanel = ref<HTMLElement | null>(null)

const navigationItems = [
  { label: 'Overview', to: '/', icon: 'i-lucide-house' },
  { label: 'Devices', to: '/devices', icon: 'i-lucide-cpu' },
] as const

const activeNavigationLabel = computed(() => {
  const activeItem = navigationItems.find(item => isNavigationActive(item.to))
  return activeItem?.label ?? 'Overview'
})

const sidebarToggleLabel = computed(() => (
  sidebarCollapsed.value ? 'Expand sidebar' : 'Collapse sidebar'
))

const toggleSidebar = () => {
  sidebarCollapsed.value = !sidebarCollapsed.value
}

function isNavigationActive(path: string): boolean {
  return path === '/' ? route.path === '/' : route.path === path || route.path.startsWith(`${path}/`)
}

function focusableElements(): HTMLElement[] {
  if (!mobileMenuPanel.value) return []
  return Array.from(mobileMenuPanel.value.querySelectorAll<HTMLElement>(
    'a[href], button:not([disabled]), [tabindex]:not([tabindex="-1"])',
  ))
}

async function openMobileMenu() {
  mobileMenuOpen.value = true
  await nextTick()
  focusableElements()[0]?.focus()
}

function closeMobileMenu(restoreFocus = true) {
  mobileMenuOpen.value = false
  if (restoreFocus) nextTick(() => mobileMenuButton.value?.focus())
}

function handleMobileMenuKeydown(event: KeyboardEvent) {
  if (!mobileMenuOpen.value) return
  if (event.key === 'Escape') {
    event.preventDefault()
    closeMobileMenu()
    return
  }
  if (event.key !== 'Tab') return
  const elements = focusableElements()
  if (elements.length === 0) return
  const first = elements[0]
  const last = elements[elements.length - 1]
  if (!first || !last) return
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault()
    last.focus()
  }
  else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault()
    first.focus()
  }
}

onMounted(() => document.addEventListener('keydown', handleMobileMenuKeydown))
onBeforeUnmount(() => document.removeEventListener('keydown', handleMobileMenuKeydown))
watch(() => route.fullPath, () => {
  if (mobileMenuOpen.value) closeMobileMenu(false)
})
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
          v-for="item in navigationItems"
          :key="item.to"
          class="app-nav-link"
          :to="item.to"
          :aria-current="isNavigationActive(item.to) ? 'page' : undefined"
          :title="sidebarCollapsed ? item.label : undefined"
        >
          <UIcon
            :name="item.icon"
            class="size-5"
            aria-hidden="true"
          />
          <span class="nav-label">{{ item.label }}</span>
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
        >{{ activeNavigationLabel }}</span>
        <button
          ref="mobileMenuButton"
          class="mobile-menu-toggle"
          type="button"
          aria-controls="mobile-navigation"
          :aria-expanded="mobileMenuOpen"
          @click="mobileMenuOpen ? closeMobileMenu() : openMobileMenu()"
        >
          <UIcon
            name="i-lucide-menu"
            class="size-5"
            aria-hidden="true"
          />
          <span>Menu</span>
        </button>
      </header>

      <div
        v-if="mobileMenuOpen"
        class="mobile-menu-layer"
      >
        <button
          class="mobile-menu-backdrop"
          type="button"
          aria-label="Close navigation"
          tabindex="-1"
          @click="closeMobileMenu()"
        />
        <aside
          id="mobile-navigation"
          ref="mobileMenuPanel"
          class="mobile-menu-panel"
          role="dialog"
          aria-modal="true"
          aria-label="Mobile navigation"
        >
          <div class="mobile-menu-header">
            <span class="mobile-menu-title">Navigation</span>
            <button
              class="mobile-menu-close"
              type="button"
              aria-label="Close navigation"
              @click="closeMobileMenu()"
            >
              <UIcon
                name="i-lucide-x"
                class="size-5"
                aria-hidden="true"
              />
            </button>
          </div>
          <nav
            class="app-nav"
            aria-label="Operations console"
          >
            <NuxtLink
              v-for="item in navigationItems"
              :key="item.to"
              class="app-nav-link"
              :to="item.to"
              :aria-current="isNavigationActive(item.to) ? 'page' : undefined"
              @click="closeMobileMenu(false)"
            >
              <UIcon
                :name="item.icon"
                class="size-5"
                aria-hidden="true"
              />
              <span class="nav-label">{{ item.label }}</span>
            </NuxtLink>
          </nav>
        </aside>
      </div>

      <main
        id="main-content"
        class="app-content"
      >
        <slot />
      </main>
    </div>
  </div>
</template>
