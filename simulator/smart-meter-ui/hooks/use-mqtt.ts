"use client"

import { useState, useCallback, useRef } from "react"
import type {
  MQTTConnectionStatus,
  DeviceConfig,
  HeartbeatMessage,
  AuthorizeRequest,
  AuthorizeResponse,
  BalanceMessage,
  UsageReport,
  InvoiceRequest,
  InvoiceResponse,
  ControlCommand,
} from "@/lib/types"

interface MQTTConfig {
  brokerUrl: string
  deviceId: string
  username?: string
  password?: string
}

interface MQTTHookReturn {
  connectionStatus: MQTTConnectionStatus
  connect: (config: MQTTConfig) => void
  disconnect: () => void
  publishHeartbeat: (message: HeartbeatMessage) => void
  publishAuthorizeRequest: (request: AuthorizeRequest) => void
  publishUsageReport: (report: UsageReport) => void
  publishInvoiceRequest: (request: InvoiceRequest) => void
  subscribeToConfig: (callback: (config: DeviceConfig) => void) => void
  subscribeToAuthorizeResponse: (callback: (response: AuthorizeResponse) => void) => void
  subscribeToBalance: (callback: (balance: BalanceMessage) => void) => void
  subscribeToInvoiceResponse: (callback: (response: InvoiceResponse) => void) => void
  subscribeToControl: (callback: (command: ControlCommand) => void) => void
  lastError: string | null
}

export function useMQTT(): MQTTHookReturn {
  const [connectionStatus, setConnectionStatus] = useState<MQTTConnectionStatus>("disconnected")
  const [lastError, setLastError] = useState<string | null>(null)
  const clientRef = useRef<unknown>(null)
  const configRef = useRef<MQTTConfig | null>(null)

  const callbacksRef = useRef<{
    config?: (config: DeviceConfig) => void
    authorizeResponse?: (response: AuthorizeResponse) => void
    balance?: (balance: BalanceMessage) => void
    invoiceResponse?: (response: InvoiceResponse) => void
    control?: (command: ControlCommand) => void
  }>({})

  const connect = useCallback((config: MQTTConfig) => {
    configRef.current = config
    setConnectionStatus("connecting")
    setLastError(null)

    // Simulate MQTT connection - in production, use mqtt.js or similar
    setTimeout(() => {
      setConnectionStatus("connected")
      console.log("[v0] MQTT Connected to", config.brokerUrl, config.username ? `(authenticated as ${config.username})` : "")
    }, 1000)
  }, [])

  const disconnect = useCallback(() => {
    setConnectionStatus("disconnected")
    configRef.current = null
    console.log("[v0] MQTT Disconnected")
  }, [])

  const publish = useCallback(
    (topic: string, payload: unknown) => {
      if (connectionStatus !== "connected") {
        console.warn("[v0] Cannot publish - not connected")
        return
      }
      console.log(`[v0] MQTT Publish: ${topic}`, payload)
    },
    [connectionStatus],
  )

  const publishHeartbeat = useCallback(
    (message: HeartbeatMessage) => {
      if (!configRef.current) return
      publish(`/devices/${configRef.current.deviceId}/heartbeat`, message)
    },
    [publish],
  )

  const publishAuthorizeRequest = useCallback(
    (request: AuthorizeRequest) => {
      if (!configRef.current) return
      publish(`/devices/${configRef.current.deviceId}/request/authorize`, request)
    },
    [publish],
  )

  const publishUsageReport = useCallback(
    (report: UsageReport) => {
      if (!configRef.current) return
      publish(`/devices/${configRef.current.deviceId}/usage`, report)
    },
    [publish],
  )

  const publishInvoiceRequest = useCallback(
    (request: InvoiceRequest) => {
      if (!configRef.current) return
      publish(`/devices/${configRef.current.deviceId}/request/invoice`, request)
    },
    [publish],
  )

  const subscribeToConfig = useCallback((callback: (config: DeviceConfig) => void) => {
    callbacksRef.current.config = callback
  }, [])

  const subscribeToAuthorizeResponse = useCallback((callback: (response: AuthorizeResponse) => void) => {
    callbacksRef.current.authorizeResponse = callback
  }, [])

  const subscribeToBalance = useCallback((callback: (balance: BalanceMessage) => void) => {
    callbacksRef.current.balance = callback
  }, [])

  const subscribeToInvoiceResponse = useCallback((callback: (response: InvoiceResponse) => void) => {
    callbacksRef.current.invoiceResponse = callback
  }, [])

  const subscribeToControl = useCallback((callback: (command: ControlCommand) => void) => {
    callbacksRef.current.control = callback
  }, [])

  return {
    connectionStatus,
    connect,
    disconnect,
    publishHeartbeat,
    publishAuthorizeRequest,
    publishUsageReport,
    publishInvoiceRequest,
    subscribeToConfig,
    subscribeToAuthorizeResponse,
    subscribeToBalance,
    subscribeToInvoiceResponse,
    subscribeToControl,
    lastError,
  }
}
