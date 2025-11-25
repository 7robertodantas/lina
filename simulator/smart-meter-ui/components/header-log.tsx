"use client"

import { Terminal, Info, AlertCircle, CheckCircle } from "lucide-react"
import { cn } from "@/lib/utils"

interface LogEntry {
  timestamp: string
  message: string
  type: "info" | "error" | "success"
}

interface HeaderLogProps {
  logs: LogEntry[]
  maxLines?: number
}

const iconMap = {
  info: Info,
  error: AlertCircle,
  success: CheckCircle,
}

export function HeaderLog({ logs, maxLines = 3 }: HeaderLogProps) {
  const recentLogs = logs.slice(-maxLines)

  return (
    <div className="flex items-center gap-3">
      <Terminal className="h-4 w-4 text-muted-foreground shrink-0" />
      <div className="flex-1 min-w-0">
        {recentLogs.length === 0 ? (
          <p className="text-xs text-muted-foreground">No events yet</p>
        ) : (
          <div className="space-y-0.5">
            {recentLogs.map((log, i) => {
              const Icon = iconMap[log.type]
              return (
                <div key={i} className="flex items-center gap-2 font-mono text-xs truncate">
                  <Icon
                    className={cn(
                      "h-3 w-3 shrink-0",
                      log.type === "error" && "text-destructive",
                      log.type === "success" && "text-accent",
                      log.type === "info" && "text-muted-foreground",
                    )}
                  />
                  <span className="text-muted-foreground shrink-0">{new Date(log.timestamp).toLocaleTimeString()}</span>
                  <span
                    className={cn(
                      "truncate",
                      log.type === "error" && "text-destructive",
                      log.type === "success" && "text-accent",
                      log.type === "info" && "text-foreground",
                    )}
                  >
                    {log.message}
                  </span>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
