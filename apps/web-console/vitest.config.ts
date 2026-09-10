import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import { defineVitestProject } from '@nuxt/test-utils/config'

process.env.NUXT_APP_ENV ??= 'test'

export default defineConfig({
  plugins: [vue()],
  test: {
    projects: [
      {
        test: {
          name: 'unit',
          include: ['test/unit/**/*.{test,spec}.ts'],
          environment: 'happy-dom',
        },
      },
      await defineVitestProject({
        test: {
          name: 'nuxt',
          include: ['test/nuxt/**/*.{test,spec}.ts'],
          environment: 'nuxt',
        },
      }),
    ],
  },
})
