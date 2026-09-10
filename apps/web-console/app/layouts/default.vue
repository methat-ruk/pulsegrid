<template>
  <div class="app-shell">
    <a
      class="skip-link"
      href="#main-content"
    >Skip to main content</a>

    <aside
      class="app-sidebar"
      aria-label="Primary navigation"
    >
      <NuxtLink
        class="brand"
        to="/"
        aria-label="PulseGrid home"
      >PulseGrid</NuxtLink>

      <nav
        class="app-nav"
        aria-label="Operations console"
      >
        <NuxtLink
          class="app-nav-link"
          to="/"
          aria-current="page"
        >
          <UIcon
            name="i-lucide-house"
            class="size-5"
            aria-hidden="true"
          />
          <span>Overview</span>
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
