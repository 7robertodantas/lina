"use client"

import { useState, useCallback, useRef, useEffect } from "react"
import { getMQTTClient } from "@/lib/mqtt"
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
  const mqttClient = useRef(getMQTTClient())
  const configRef = useRef<MQTTConfig | null>(null)

  const callbacksRef = useRef<{
    config?: (config: DeviceConfig) => void
    authorizeResponse?: (response: AuthorizeResponse) => void
    balance?: (balance: BalanceMessage) => void
    invoiceResponse?: (response: InvoiceResponse) => void
    control?: (command: ControlCommand) => void
  }>({})

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      mqttClient.current.disconnect()
    }
  }, [])

  const handleMessage = useCallback((topic: string, message: Buffer) => {
    try {
      const payload = JSON.parse(message.toString())
      console.log(`[MQTT] Message received on ${topic}:`, payload)

      // Route messages to appropriate callbacks based on topic
      if (topic.includes("/config")) {
        callbacksRef.current.config?.(payload as DeviceConfig)
      } else if (topic.includes("/response/authorize")) {
        callbacksRef.current.authorizeResponse?.(payload as AuthorizeResponse)
      } else if (topic.includes("/balance")) {
        callbacksRef.current.balance?.(payload as BalanceMessage)
      } else if (topic.includes("/response/invoice")) {
        callbacksRef.current.invoiceResponse?.(payload as InvoiceResponse)
      } else if (topic.includes("/control")) {
        callbacksRef.current.control?.(payload as ControlCommand)
      }
    } catch (error) {
      console.error("[MQTT] Error parsing message:", error)
    }
  }, [])

  const connect = useCallback((config: MQTTConfig) => {
    configRef.current = config
    setConnectionStatus("connecting")
    setLastError(null)

    mqttClient.current.connect({
      brokerUrl: config.brokerUrl,
      username: config.username,
      password: config.password,
      clientId: `${config.deviceId}_${Date.now()}`,
      onConnect: () => {
        setConnectionStatus("connected")
        
        // Auto-subscribe to relevant topics after a brief delay to ensure connection is fully ready
        if (configRef.current) {
          const deviceId = configRef.current.deviceId
          // Small delay to ensure connection is fully established
          setTimeout(() => {
            console.log(`[MQTT] Subscribing to topics for device: ${deviceId}`)
            // Try wildcard subscription first (should be allowed by ACL: /devices/{deviceId}/#)
            // This is more efficient and should work if ACLs are properly configured
            const wildcardTopic = `/devices/${deviceId}/#`
            mqttClient.current.subscribe(wildcardTopic, 0, (error) => {
              if (error) {
                console.error(`[MQTT] Wildcard subscription failed, trying individual topics:`, error)
                // Fallback to individual topic subscriptions
                mqttClient.current.subscribe(`/devices/${deviceId}/config`)
                mqttClient.current.subscribe(`/devices/${deviceId}/response/authorize`)
                mqttClient.current.subscribe(`/devices/${deviceId}/balance`)
                mqttClient.current.subscribe(`/devices/${deviceId}/response/invoice`)
                mqttClient.current.subscribe(`/devices/${deviceId}/control`)
              } else {
                console.log(`[MQTT] Successfully subscribed to wildcard: ${wildcardTopic}`)
              }
            })
          }, 100)
        }
      },
      onDisconnect: () => {
        setConnectionStatus("disconnected")
      },
      onError: (error) => {
        setLastError(error.message)
        setConnectionStatus("error")
      },
      onMessage: handleMessage,
    })
  }, [handleMessage])

  const disconnect = useCallback(() => {
    mqttClient.current.disconnect()
    setConnectionStatus("disconnected")
    configRef.current = null
  }, [])

  const publish = useCallback(
    (topic: string, payload: unknown) => {
      mqttClient.current.publish(topic, payload as string | object)
    },
    [],
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
