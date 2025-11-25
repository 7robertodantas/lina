"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { cn } from "@/lib/utils"
import { Zap, Activity } from "lucide-react"

interface MainMeterProps {
  instantPower: number
  totalConsumption: number
  isOnline: boolean
}

export function MainMeter({ instantPower, totalConsumption, isOnline }: MainMeterProps) {
  const powerPercentage = Math.min((instantPower / 5000) * 100, 100)

  return (
    <Card className="border-border bg-card">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
          <Activity className="h-4 w-4" />
          DIN Rail Energy Meter
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Main Power Display */}
        <div className="relative flex flex-col items-center justify-center rounded-lg border border-border bg-secondary/50 p-6">
          <div className="absolute top-2 right-2 flex items-center gap-1.5">
            <div className={cn("h-2 w-2 rounded-full", isOnline ? "bg-accent animate-pulse" : "bg-muted")} />
            <span className="text-xs text-muted-foreground">{isOnline ? "ONLINE" : "OFFLINE"}</span>
          </div>

          <div className="flex items-baseline gap-1">
            <Zap className={cn("h-8 w-8", instantPower > 0 ? "text-primary" : "text-muted-foreground")} />
            <span className="font-mono text-5xl font-bold tracking-tight text-foreground">
              {instantPower.toLocaleString()}
            </span>
            <span className="text-xl text-muted-foreground">W</span>
          </div>

          <p className="mt-1 text-sm text-muted-foreground">Instant Power</p>

          {/* Power Bar */}
          <div className="mt-4 h-2 w-full overflow-hidden rounded-full bg-secondary">
            <div className="h-full bg-primary transition-all duration-500" style={{ width: `${powerPercentage}%` }} />
          </div>
          <div className="mt-1 flex w-full justify-between text-xs text-muted-foreground">
            <span>0W</span>
            <span>5000W</span>
          </div>
        </div>

        {/* Total Consumption */}
        <div className="flex items-center justify-between rounded-lg border border-border bg-secondary/30 p-4">
          <div>
            <p className="text-sm text-muted-foreground">Total Consumption</p>
            <p className="font-mono text-2xl font-semibold text-foreground">
              {totalConsumption.toFixed(4)}
              <span className="ml-1 text-sm text-muted-foreground">kWh</span>
            </p>
          </div>
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-primary/10">
            <Activity className="h-6 w-6 text-primary" />
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
