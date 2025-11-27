"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Terminal, Info, AlertCircle, CheckCircle } from "lucide-react"
import { cn } from "@/lib/utils"

interface LogEntry {
  timestamp: string
  message: string
  type: "info" | "error" | "success"
}

interface EventLogProps {
  logs: LogEntry[]
}

const iconMap = {
  info: Info,
  error: AlertCircle,
  success: CheckCircle,
}

export function EventLog({ logs }: EventLogProps) {
  return (
    <Card className="border-border bg-card">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
          <Terminal className="h-4 w-4" />
          Event Log
        </CardTitle>
      </CardHeader>
      <CardContent>
        <ScrollArea className="h-48 rounded-md border border-border bg-secondary/30 p-2">
          {logs.length === 0 ? (
            <p className="py-4 text-center text-sm text-muted-foreground">No events yet. Start the meter to begin.</p>
          ) : (
            <div className="space-y-1">
              {logs.map((log, i) => {
                const Icon = iconMap[log.type]
                return (
                  <div key={i} className="flex items-start gap-2 rounded px-2 py-1 font-mono text-xs">
                    <Icon
                      className={cn(
                        "mt-0.5 h-3 w-3 shrink-0",
                        log.type === "error" && "text-destructive",
                        log.type === "success" && "text-accent",
                        log.type === "info" && "text-muted-foreground",
                      )}
                    />
                    <span className="text-muted-foreground">{new Date(log.timestamp).toLocaleTimeString()}</span>
                    <span
                      className={cn(
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
        </ScrollArea>
      </CardContent>
    </Card>
  )
}
