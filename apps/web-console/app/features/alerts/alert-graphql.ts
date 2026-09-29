import { executeGraphQL } from '../graphql/client'

export type ThresholdComparator = 'GT' | 'GTE' | 'LT' | 'LTE'
export type ThresholdMetric = 'TEMPERATURE_CELSIUS'

export interface AlertOccurrence {
  id: string
  deviceId: string
  ruleId: string
  messageId: string
  observedAt: string
  receivedAt: string
  temperatureCelsius: number
  metric: ThresholdMetric
  comparator: ThresholdComparator
  thresholdCelsius: number
  createdAt: string
}

export interface AlertEdge {
  cursor: string
  node: AlertOccurrence
}

export interface AlertPageInfo {
  endCursor: string | null
  hasNextPage: boolean
}

export interface AlertConnection {
  edges: AlertEdge[]
  pageInfo: AlertPageInfo
}

export const alertsQuery = `query Alerts($first: Int!, $after: String, $deviceId: ID) {
  alerts(first: $first, after: $after, deviceId: $deviceId) {
    edges {
      cursor
      node {
        id
        deviceId
        temperatureCelsius
        metric
        comparator
        thresholdCelsius
        createdAt
      }
    }
    pageInfo {
      endCursor
      hasNextPage
    }
  }
}`

export const alertQuery = `query Alert($id: ID!) {
  alert(id: $id) {
    id
    deviceId
    ruleId
    messageId
    observedAt
    receivedAt
    temperatureCelsius
    metric
    comparator
    thresholdCelsius
    createdAt
  }
}`

export function listAlerts(
  first = 50,
  after: string | null = null,
  deviceId: string | null = null,
  signal?: AbortSignal,
) {
  return executeGraphQL<{ alerts: AlertConnection }, { first: number, after: string | null, deviceId: string | null }>(
    alertsQuery,
    { first, after, deviceId },
    { signal, serviceName: 'alert' },
  )
}

export function getAlert(id: string, signal?: AbortSignal) {
  return executeGraphQL<{ alert: AlertOccurrence | null }, { id: string }>(
    alertQuery,
    { id },
    { signal, serviceName: 'alert' },
  )
}
