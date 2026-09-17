<template>
  <section
    class="registry-entry"
    aria-labelledby="registry-entry-title"
  >
    <div
      class="registry-entry-icon"
      aria-hidden="true"
    >
      <UIcon
        name="i-lucide-cpu"
        class="size-8"
      />
    </div>
    <div class="registry-entry-content">
      <p class="eyebrow">
        Operator workflow
      </p>
      <h2
        id="registry-entry-title"
        class="registry-entry-title"
      >
        Device registry
      </h2>
      <p class="registry-entry-description">
        List, provision, and inspect devices in the development registry.
      </p>
      <NuxtLink
        class="primary-action"
        to="/devices"
      >
        Open devices
        <UIcon
          name="i-lucide-arrow-up-right"
          class="size-4"
          aria-hidden="true"
        />
      </NuxtLink>
    </div>
  </section>
</template>
