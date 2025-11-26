"use client"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import type { BalanceMessage } from "@/lib/types"
import { Wallet, Plus, AlertTriangle } from "lucide-react"
import { cn } from "@/lib/utils"

interface BalancePanelProps {
  balance: BalanceMessage | null
  onRequestTopUp: (amount: number) => void
  isOnline: boolean
}

export function BalancePanel({ balance, onRequestTopUp, isOnline }: BalancePanelProps) {
  const availableSats = balance?.available_msat ? Math.floor(balance.available_msat / 1000) : 0
  const isLowBalance = balance && balance.available_msat < 100000
  const isOutOfFunds = balance && balance.available_msat <= 0

  return (
    <Card className="border-border bg-card">
      <CardHeader className="pb-2">
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
          <Wallet className="h-4 w-4" />
          Meter Balance
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div
          className={cn(
            "rounded-lg border p-4",
            isOutOfFunds
              ? "border-destructive bg-destructive/10"
              : isLowBalance
                ? "border-primary bg-primary/10"
                : "border-border bg-secondary/30",
          )}
        >
          {isOutOfFunds && (
            <div className="mb-2 flex items-center gap-2 text-destructive">
              <AlertTriangle className="h-4 w-4" />
              <span className="text-xs font-medium">OUT OF FUNDS</span>
            </div>
          )}
          {isLowBalance && !isOutOfFunds && (
            <div className="mb-2 flex items-center gap-2 text-primary">
              <AlertTriangle className="h-4 w-4" />
              <span className="text-xs font-medium">LOW BALANCE</span>
            </div>
          )}
          <div className="flex items-baseline gap-1">
            <span className="font-mono text-3xl font-bold text-foreground">{(availableSats ?? 0).toLocaleString()}</span>
            <span className="text-sm text-muted-foreground">sats</span>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            {(balance?.available_msat ?? 0).toLocaleString()} msat available
          </p>
        </div>

        <div className="grid grid-cols-3 gap-2">
          {[1000, 5000, 10000].map((amount) => (
            <Button
              key={amount}
              variant="outline"
              size="sm"
              className="font-mono bg-transparent"
              onClick={() => onRequestTopUp(amount * 1000)}
              disabled={!isOnline}
            >
              <Plus className="mr-1 h-3 w-3" />
              {amount}
            </Button>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}
