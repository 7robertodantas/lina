"use client"

import type React from "react"

import { Card, CardContent } from "@/components/ui/card"
import { Switch } from "@/components/ui/switch"
import { cn } from "@/lib/utils"
import type { Appliance } from "@/lib/types"
import { Refrigerator, Microwave, Flame, CookingPot, Monitor, WashingMachine } from "lucide-react"

const iconMap: Record<string, React.ComponentType<{ className?: string }>> = {
  fridge: Refrigerator,
  microwave: Microwave,
  heater: Flame,
  oven: CookingPot,
  computer: Monitor,
  washer: WashingMachine,
}

interface ApplianceCardProps {
  appliance: Appliance
  onToggle: () => void
  disabled: boolean
}

export function ApplianceCard({ appliance, onToggle, disabled }: ApplianceCardProps) {
  const Icon = iconMap[appliance.icon] || Monitor

  return (
    <Card
      className={cn(
        "border-border bg-card transition-all duration-300",
        appliance.isOn && "border-primary/50 bg-primary/5",
      )}
    >
      <CardContent className="p-3">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div
              className={cn(
                "flex h-8 w-8 items-center justify-center rounded-lg",
                appliance.isOn ? "bg-primary/20" : "bg-secondary",
              )}
            >
              <Icon className={cn("h-4 w-4", appliance.isOn ? "text-primary" : "text-muted-foreground")} />
            </div>
            <div className="leading-tight">
              <h3 className="text-sm font-medium text-foreground">{appliance.name}</h3>
              <p className="text-xs text-muted-foreground">
                {appliance.minWatts}-{appliance.maxWatts}W
              </p>
            </div>
          </div>
          <Switch checked={appliance.isOn} onCheckedChange={onToggle} disabled={disabled} />
        </div>

        {appliance.isOn && (
          <div className="mt-1.5 rounded-md bg-secondary/50 px-2 py-0.5">
            <div className="flex items-baseline justify-between">
              <span className="text-xs text-muted-foreground">Current</span>
              <span className="font-mono text-sm font-semibold text-primary">
                {appliance.currentWatts}
                <span className="ml-0.5 text-xs text-muted-foreground">W</span>
              </span>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
