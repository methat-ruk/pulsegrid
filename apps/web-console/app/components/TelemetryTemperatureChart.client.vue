<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import type { ECharts, EChartsOption } from 'echarts'

import type { TelemetryPoint } from '../features/devices/device-graphql'

const props = defineProps<{ points: TelemetryPoint[] }>()

const chartElement = ref<HTMLElement | null>(null)
let chart: ECharts | undefined
let resizeObserver: ResizeObserver | undefined
let disposed = false

const chartPoints = computed(() => props.points
  .filter(point => Number.isFinite(Date.parse(point.observedAt)) && Number.isFinite(point.temperatureCelsius))
  .map(point => ({
    ...point,
    timestamp: Date.parse(point.observedAt),
  }))
  .sort((left, right) => left.timestamp - right.timestamp || left.messageId.localeCompare(right.messageId)))

const chartDescription = computed(() => chartPoints.value.length > 1
  ? `Temperature trend for ${chartPoints.value.length} observations.`
  : 'Temperature chart unavailable until two valid observations are loaded.')

function prefersReducedMotion(): boolean {
  return window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
}

function chartOption(): EChartsOption {
  return {
    animation: !prefersReducedMotion(),
    aria: {
      enabled: true,
      description: chartDescription.value,
    },
    grid: {
      top: 20,
      right: 20,
      bottom: 38,
      left: 56,
      containLabel: true,
    },
    tooltip: {
      trigger: 'axis',
      valueFormatter: value => `${Number(value).toLocaleString(undefined, { maximumFractionDigits: 2 })}°C`,
    },
    xAxis: {
      type: 'time',
      axisLabel: { color: '#7a8781', hideOverlap: true },
      axisLine: { lineStyle: { color: '#dde4de' } },
      splitLine: { show: false },
    },
    yAxis: {
      type: 'value',
      name: '°C',
      nameTextStyle: { color: '#56645e' },
      axisLabel: { color: '#7a8781' },
      axisLine: { show: false },
      splitLine: { lineStyle: { color: '#e3e8e3' } },
    },
    series: [{
      name: 'Temperature',
      type: 'line',
      smooth: false,
      showSymbol: true,
      symbolSize: 7,
      itemStyle: { color: '#2e9b72' },
      lineStyle: { color: '#2e9b72', width: 3 },
      data: chartPoints.value.map(point => [point.timestamp, point.temperatureCelsius]),
    }],
  }
}

function renderChart() {
  if (!chart || chartPoints.value.length < 2) return
  chart.setOption(chartOption(), true)
}

onMounted(async () => {
  await nextTick()
  if (!chartElement.value) return
  try {
    const [core, charts, components, renderers] = await Promise.all([
      import('echarts/core'),
      import('echarts/charts'),
      import('echarts/components'),
      import('echarts/renderers'),
    ])
    if (disposed || !chartElement.value) return
    core.use([
      charts.LineChart,
      components.AriaComponent,
      components.GridComponent,
      components.TooltipComponent,
      renderers.SVGRenderer,
    ])
    chart = core.init(chartElement.value, undefined, { renderer: 'svg' })
    renderChart()
    if (typeof ResizeObserver !== 'undefined') {
      resizeObserver = new ResizeObserver(() => chart?.resize())
      resizeObserver.observe(chartElement.value)
    }
  }
  catch {
    chartElement.value?.setAttribute('aria-label', 'Temperature chart unavailable.')
  }
})

watch(() => props.points, renderChart, { deep: true })

onBeforeUnmount(() => {
  disposed = true
  resizeObserver?.disconnect()
  resizeObserver = undefined
  chart?.dispose()
  chart = undefined
})
</script>

<template>
  <div
    ref="chartElement"
    class="telemetry-chart"
    role="img"
    :aria-label="chartDescription"
  />
</template>
