import mqtt from "mqtt"
import type { MqttClient, IClientOptions } from "mqtt"

export interface MQTTClientConfig {
  brokerUrl: string
  username?: string
  password?: string
  clientId?: string
  onConnect?: () => void
  onDisconnect?: () => void
  onError?: (error: Error) => void
  onMessage?: (topic: string, message: Buffer) => void
}

export class MQTTClientWrapper {
  private client: MqttClient | null = null
  private config: MQTTClientConfig | null = null

  connect(config: MQTTClientConfig): void {
    if (this.client?.connected) {
      console.warn("[MQTT] Already connected")
      return
    }

    this.config = config

    const options: IClientOptions = {
      clientId: config.clientId || `mqtt_client_${Math.random().toString(16).slice(2, 10)}`,
      clean: true,
      reconnectPeriod: 5000,
      connectTimeout: 30000,
    }

    // Add authentication if provided
    if (config.username) {
      options.username = config.username
    }
    if (config.password) {
      options.password = config.password
    }

    console.log("[MQTT] Connecting to", config.brokerUrl)
    console.log("[MQTT] Username:", config.username || "(none)")
    this.client = mqtt.connect(config.brokerUrl, options)

    this.client.on("connect", () => {
      console.log("[MQTT] Connected successfully")
      config.onConnect?.()
    })

    this.client.on("error", (error) => {
      console.error("[MQTT] Connection error:", error)
      config.onError?.(error)
    })

    this.client.on("close", () => {
      console.log("[MQTT] Connection closed")
      config.onDisconnect?.()
    })

    this.client.on("offline", () => {
      console.log("[MQTT] Client offline")
    })

    this.client.on("reconnect", () => {
      console.log("[MQTT] Reconnecting...")
    })

    this.client.on("message", (topic, message) => {
      config.onMessage?.(topic, message)
    })
  }

  disconnect(): void {
    if (this.client) {
      console.log("[MQTT] Disconnecting...")
      this.client.end(true)
      this.client = null
      this.config = null
    }
  }

  publish(topic: string, message: string | object, qos: 0 | 1 | 2 = 0): void {
    if (!this.client?.connected) {
      console.warn("[MQTT] Cannot publish - not connected")
      return
    }

    const payload = typeof message === "string" ? message : JSON.stringify(message)
    
    this.client.publish(topic, payload, { qos }, (error) => {
      if (error) {
        console.error(`[MQTT] Publish error on topic ${topic}:`, error)
      } else {
        console.log(`[MQTT] Published to ${topic}:`, message)
      }
    })
  }

  subscribe(topic: string, qos: 0 | 1 | 2 = 0, callback?: (error: Error | null, granted?: any) => void): void {
    if (!this.client?.connected) {
      console.warn("[MQTT] Cannot subscribe - not connected")
      callback?.(new Error("Not connected"))
      return
    }

    this.client.subscribe(topic, { qos }, (error, granted) => {
      if (error) {
        console.error(`[MQTT] Subscribe error for topic ${topic}:`, error)
        // Also check if error has additional details
        if ('code' in error) {
          console.error(`[MQTT] Error code:`, (error as any).code)
        }
        if ('message' in error) {
          console.error(`[MQTT] Error message:`, (error as any).message)
        }
        // Call onError if available
        this.config?.onError?.(error)
        return
      }

      // Check granted QoS levels - 128 indicates subscription failure
      if (granted && granted.length > 0) {
        const grant = granted[0]
        if (grant.qos === 128) {
          const errorMsg = `Subscription denied for topic ${topic} (QoS 128 = denied). The device may not be provisioned correctly or subscribe ACL permissions are missing.`
          console.error(`[MQTT] ${errorMsg}`)
          console.error(`[MQTT] Username: ${this.config?.username || 'unknown'}`)
          console.error(`[MQTT] Tip: Check device service logs when provisioning to ensure subscribe ACLs were added successfully`)
          console.error(`[MQTT] Tip: Verify the role 'device_${this.config?.username || 'unknown'}_role' has subscribePattern ACLs`)
          const subscriptionError = new Error(errorMsg)
          this.config?.onError?.(subscriptionError)
          callback?.(subscriptionError)
          return
        }
        if (grant.qos !== qos) {
          console.warn(`[MQTT] QoS downgraded for topic ${topic}: requested ${qos}, granted ${grant.qos}`)
        }
      }

      console.log(`[MQTT] Subscribed to ${topic} (QoS: ${granted?.[0]?.qos ?? qos})`)
      callback?.(null, granted)
    })
  }

  unsubscribe(topic: string): void {
    if (!this.client?.connected) {
      console.warn("[MQTT] Cannot unsubscribe - not connected")
      return
    }

    this.client.unsubscribe(topic, (error) => {
      if (error) {
        console.error(`[MQTT] Unsubscribe error for topic ${topic}:`, error)
      } else {
        console.log(`[MQTT] Unsubscribed from ${topic}`)
      }
    })
  }

  isConnected(): boolean {
    return this.client?.connected ?? false
  }
}

// Singleton instance for browser usage
let mqttClientInstance: MQTTClientWrapper | null = null

export function getMQTTClient(): MQTTClientWrapper {
  if (!mqttClientInstance) {
    mqttClientInstance = new MQTTClientWrapper()
  }
  return mqttClientInstance
}
